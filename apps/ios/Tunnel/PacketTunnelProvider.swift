import Darwin
import Foundation
import MobileCore
import Network
import NetworkExtension

private enum TunnelFailure: Int, Error {
    case access = 1, network, engine, stopped
}

/// The bridge calls this for sockets to the single allowed Xray endpoint.
private final class Uplink: NSObject, AndroidbridgeSocketProtectorProtocol {
    private let index: UInt32
    init(index: UInt32) { self.index = index }

    func protect(_ fd: Int32) -> Bool {
        var address = sockaddr_storage()
        var length = socklen_t(MemoryLayout<sockaddr_storage>.size)
        guard withUnsafeMutablePointer(to: &address, {
            $0.withMemoryRebound(to: sockaddr.self, capacity: 1) { getsockname(fd, $0, &length) }
        }) == 0 else { return false }
        var value = index
        switch Int32(address.ss_family) {
        case AF_INET:
            return setsockopt(fd, IPPROTO_IP, IP_BOUND_IF, &value, socklen_t(MemoryLayout<UInt32>.size)) == 0
        case AF_INET6:
            return setsockopt(fd, IPPROTO_IPV6, IPV6_BOUND_IF, &value, socklen_t(MemoryLayout<UInt32>.size)) == 0
        default:
            return false
        }
    }
}

final class PacketTunnelProvider: NEPacketTunnelProvider {
    private let queue = DispatchQueue(label: "org.ovpn.packet-tunnel")
    private var monitor: NWPathMonitor?
    private var bridge: PacketBridge?
    private var completion: ((Error?) -> Void)?
    private var generation = UUID()
    private var uplink: UInt32 = 0

    override func startTunnel(options: [String: NSObject]?, completionHandler: @escaping (Error?) -> Void) {
        queue.async { [self] in
            guard self.completion == nil, self.bridge == nil else {
                completionHandler(TunnelFailure.engine)
                return
            }
            self.completion = completionHandler
            let run = UUID()
            self.generation = run
            let monitor = NWPathMonitor()
            self.monitor = monitor
            monitor.pathUpdateHandler = { [weak self] path in
                guard let self, self.generation == run else { return }
                let physical = path.availableInterfaces.first {
                    [.wifi, .cellular, .wiredEthernet].contains($0.type) && path.usesInterfaceType($0.type)
                }
                let index = physical.map { if_nametoindex($0.name) } ?? 0
                if self.bridge != nil {
                    if index != self.uplink {
                        self.cancelTunnelWithError(TunnelFailure.network)
                    }
                } else if self.uplink == 0, path.status == .satisfied, index != 0 {
                    self.uplink = index
                    self.configure(run: run, index: index)
                }
            }
            monitor.start(queue: self.queue)
            self.queue.asyncAfter(deadline: .now() + 15) {
                if self.generation == run, self.completion != nil { self.fail(TunnelFailure.network) }
            }
        }
    }

    private func configure(run: UUID, index: UInt32) {
        guard let raw = try? AccessStorage.read("active-profile"),
              let link = String(data: raw, encoding: .utf8),
              let url = URLComponents(string: link), let host = url.host,
              let endpoint = resolve(host) else {
            fail(TunnelFailure.access)
            return
        }
        let settings = NEPacketTunnelNetworkSettings(tunnelRemoteAddress: endpoint)
        settings.mtu = 1280
        let ipv4 = NEIPv4Settings(addresses: ["198.18.0.1"], subnetMasks: ["255.255.255.252"])
        ipv4.includedRoutes = [NEIPv4Route.default()]
        settings.ipv4Settings = ipv4
        let ipv6 = NEIPv6Settings(addresses: ["fd6d:9b2a:4f00::1"], networkPrefixLengths: [126])
        ipv6.includedRoutes = [NEIPv6Route.default()]
        settings.ipv6Settings = ipv6
        settings.dnsSettings = NEDNSSettings(servers: ["1.1.1.1"])

        setTunnelNetworkSettings(settings) { [weak self] error in
            guard let self else { return }
            self.queue.async {
                guard self.generation == run else { return }
                guard error == nil, let bridge = try? PacketBridge(flow: self.packetFlow, queue: self.queue) else {
                    self.fail(TunnelFailure.engine)
                    return
                }
                self.bridge = bridge
                let issue = AndroidbridgeStart(link, endpoint, bridge.engine, Uplink(index: index))
                guard issue.isEmpty else {
                    self.fail(TunnelFailure.engine)
                    return
                }
                bridge.start()
                self.completion?(nil)
                self.completion = nil
            }
        }
    }

    private func resolve(_ hostname: String) -> String? {
        var hints = addrinfo()
        hints.ai_family = AF_UNSPEC
        hints.ai_socktype = SOCK_STREAM
        var resolved: UnsafeMutablePointer<addrinfo>?
        guard getaddrinfo(hostname, nil, &hints, &resolved) == 0, let resolved else { return nil }
        defer { freeaddrinfo(resolved) }
        var text = [CChar](repeating: 0, count: Int(NI_MAXHOST))
        guard getnameinfo(resolved.pointee.ai_addr, resolved.pointee.ai_addrlen,
                          &text, socklen_t(text.count), nil, 0, NI_NUMERICHOST) == 0 else { return nil }
        return String(cString: text)
    }

    private func fail(_ error: Error) {
        let finish = completion
        completion = nil
        cleanup()
        finish?(error)
    }

    private func cleanup() {
        generation = UUID()
        uplink = 0
        monitor?.cancel()
        monitor = nil
        AndroidbridgeStop()
        bridge?.stop()
        bridge = nil
    }

    override func stopTunnel(with reason: NEProviderStopReason, completionHandler: @escaping () -> Void) {
        queue.async {
            self.fail(TunnelFailure.stopped)
            completionHandler()
        }
    }
}
