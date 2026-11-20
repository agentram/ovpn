import Darwin
import Foundation
import NetworkExtension

/// Moves public Network Extension packets into the engine's Darwin packet socket.
final class PacketBridge {
    enum Failure: Error { case socket }

    let engine: Int32
    private let flow: NEPacketTunnelFlow
    private let socket: Int32
    private let queue: DispatchQueue
    private var reader: DispatchSourceRead?
    private var closed = false

    init(flow: NEPacketTunnelFlow, queue: DispatchQueue) throws {
        var pair: [Int32] = [-1, -1]
        guard socketpair(AF_UNIX, SOCK_DGRAM, 0, &pair) == 0 else { throw Failure.socket }
        guard fcntl(pair[0], F_SETFL, O_NONBLOCK) == 0 else {
            Darwin.close(pair[0])
            Darwin.close(pair[1])
            throw Failure.socket
        }
        self.flow = flow
        self.socket = pair[0]
        self.engine = pair[1]
        self.queue = queue
    }

    func start() {
        let reader = DispatchSource.makeReadSource(fileDescriptor: socket, queue: queue)
        reader.setEventHandler { [weak self] in self?.writeToVPN() }
        self.reader = reader
        reader.activate()
        readFromVPN()
    }

    private func readFromVPN() {
        flow.readPackets { [weak self] packets, protocols in
            guard let self else { return }
            self.queue.async {
                guard !self.closed else { return }
                for (packet, proto) in zip(packets, protocols) {
                    let family = proto.int32Value
                    guard family == AF_INET || family == AF_INET6, packet.count <= 1280 else { continue }
                    var frame = Data([0, 0, 0, UInt8(family)])
                    frame.append(packet)
                    frame.withUnsafeBytes { _ = Darwin.send(self.socket, $0.baseAddress, $0.count, 0) }
                }
                self.readFromVPN()
            }
        }
    }

    private func writeToVPN() {
        var buffer = [UInt8](repeating: 0, count: 1284)
        var packets: [Data] = []
        var families: [NSNumber] = []
        for _ in 0..<32 {
            let amount = buffer.withUnsafeMutableBytes { Darwin.recv(socket, $0.baseAddress, $0.count, 0) }
            guard amount > 4 else { break }
            let family = Int32(buffer[3])
            guard family == AF_INET || family == AF_INET6 else { continue }
            packets.append(Data(buffer[4..<amount]))
            families.append(NSNumber(value: family))
        }
        if !packets.isEmpty { _ = flow.writePackets(packets, withProtocols: families) }
    }

    func stop() {
        guard !closed else { return }
        closed = true
        let descriptor = socket
        if let reader {
            reader.setCancelHandler { Darwin.close(descriptor) }
            reader.cancel()
            self.reader = nil
        } else {
            Darwin.close(descriptor)
        }
        Darwin.close(engine)
    }
}
