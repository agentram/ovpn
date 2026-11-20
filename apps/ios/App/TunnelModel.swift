import AppAuth
import Foundation
import MobileCore
import NetworkExtension
import SwiftUI

struct MobileProfile: Codable, Identifiable, Equatable {
    let id: String
    let name: String
    let link: String
}

private struct Inspection: Decodable {
    struct Row: Decodable {
        let id: String?
        let name: String
        let link: String?
        let issue: String?
    }
    let profiles: [Row]?
}

private struct Snapshot: Codable {
    var pending: Setup?
    var profiles: [MobileProfile] = []
    var selected: String?
}

@MainActor
final class TunnelModel: ObservableObject {
    enum Phase: String {
        case disconnected, connecting, checking, connected, unconfirmed, disconnecting
    }

    @Published private(set) var signedIn = false
    @Published private(set) var busy = false
    @Published private(set) var profiles: [MobileProfile] = []
    @Published private(set) var selected: MobileProfile?
    @Published private(set) var phase: Phase = .disconnected
    @Published var issue: String?
    @Published var code = ""
    @Published private(set) var canResume = false

    let account = AccountClient()
    private var auth: OIDAuthState?
    private var snapshot = Snapshot()
    private var manager: NETunnelProviderManager?
    private var verifier: Task<Void, Never>?
    private var stopRequested = false
    private var evidence: [String] = []

    func load() async {
        do {
            if let data = try AccessStorage.read("account") {
                auth = try NSKeyedUnarchiver.unarchivedObject(ofClass: OIDAuthState.self, from: data)
            }
            if auth != nil, let data = try AccessStorage.read("profiles") {
                snapshot = try JSONDecoder().decode(Snapshot.self, from: data)
            }
            signedIn = auth != nil
            presentSnapshot()
            manager = try await NETunnelProviderManager.loadAllFromPreferences().first {
                ($0.protocolConfiguration as? NETunnelProviderProtocol)?.providerBundleIdentifier == Self.provider
            }
            statusChanged()
            if !signedIn { manager?.connection.stopVPNTunnel() }
        } catch {
            show(.storage)
        }
    }

    func signIn() async {
        await perform {
            let newAuth = try await account.signIn()
            let token = try await account.freshToken(newAuth)
            try await account.verify(token)
            let profiles: [MobileProfile]
            do { profiles = try await fetch(token: token) }
            catch Issue.used { profiles = [] }
            stop()
            try store(Snapshot(profiles: profiles, selected: profiles.first?.id))
            try AccessStorage.write("active-profile", nil)
            try saveAuth(newAuth)
            auth = newAuth
            signedIn = true
        }
    }

    func signOut() async {
        await perform {
            stop()
            try AccessStorage.write("active-profile", nil)
            try AccessStorage.write("account", nil)
            auth = nil
            signedIn = false
            try store(Snapshot())
        }
    }

    func activate() async {
        await perform {
            guard let origin = account.origin else { throw Issue.config }
            let value = code.trimmingCharacters(in: .whitespacesAndNewlines)
            let setup: Setup
            if value.isEmpty, let pending = snapshot.pending {
                setup = pending
            } else {
                let parsed = try Setup.parse(value, origin: origin)
                setup = snapshot.pending.flatMap { $0.code == parsed.code ? $0 : nil } ?? parsed
            }
            let token = try await token()
            var next = snapshot
            next.pending = setup
            try store(next)
            do {
                try await account.activate(setup, token: token)
            } catch Issue.used {
                next.pending = nil
                try store(next)
                throw Issue.used
            }
            let profiles = try await fetch(token: token)
            stop()
            try store(Snapshot(profiles: profiles, selected: profiles.first?.id))
            code = ""
        }
    }

    func refresh() async {
        await perform {
            let profiles = try await fetch(token: token())
            var next = snapshot
            next.profiles = profiles
            if !profiles.contains(where: { $0.id == snapshot.selected }) {
                stop()
                try AccessStorage.write("active-profile", nil)
                next.selected = nil
            }
            try store(next)
        }
    }

    func select(_ profile: MobileProfile) async {
        await perform {
            guard profile.id != selected?.id else { return }
            if phase != .disconnected { stop() }
            var next = snapshot
            next.selected = profile.id
            try store(next)
        }
    }

    func connect() async {
        await perform {
            guard signedIn, let selected, phase == .disconnected else { return }
            let tunnel = manager ?? NETunnelProviderManager()
            let configuration = NETunnelProviderProtocol()
            configuration.providerBundleIdentifier = Self.provider
            configuration.serverAddress = "O Tunnel"
            tunnel.protocolConfiguration = configuration
            tunnel.localizedDescription = "O Tunnel"
            tunnel.isEnabled = true
            try AccessStorage.write("active-profile", Data(selected.link.utf8))
            do {
                try await tunnel.saveToPreferences()
                try await tunnel.loadFromPreferences()
                manager = tunnel
                stopRequested = false
                transition(.connecting)
                try tunnel.connection.startVPNTunnel()
            } catch {
                transition(.disconnected)
                throw Issue.connect
            }
        }
    }

    func stop() {
        stopRequested = true
        verifier?.cancel()
        verifier = nil
        manager?.connection.stopVPNTunnel()
        if let connection = manager?.connection, connection.status != .disconnected && connection.status != .invalid {
            transition(.disconnecting)
        } else {
            transition(.disconnected)
        }
    }

    func statusChanged() {
        guard let connection = manager?.connection else { return }
        switch connection.status {
        case .connected:
            guard !stopRequested else { return }
            if verifier == nil {
                verifier = Task { await verify() }
            }
        case .connecting, .reasserting:
            transition(.connecting)
        case .disconnecting:
            transition(.disconnecting)
        case .invalid, .disconnected:
            verifier?.cancel()
            verifier = nil
            if !stopRequested && phase != .disconnected { show(.connect) }
            transition(.disconnected)
        @unknown default: break
        }
    }

    var report: String {
        let version = Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? ""
        return (["O Tunnel", "App: \(version)", "Engine: \(AndroidbridgeCoreVersion())",
                 "iOS: \(UIDevice.current.systemVersion)"] + evidence).joined(separator: "\n")
    }

    private static let provider = "io.github.agentram.ovpn.tunnel.PacketTunnel"

    private func fetch(token: String) async throws -> [MobileProfile] {
        let list = try await account.profiles(token: token)
        let output = AndroidbridgeInspect(list)
        let parsed = try JSONDecoder().decode(Inspection.self, from: Data(output.utf8))
        let profiles = parsed.profiles?.compactMap { row -> MobileProfile? in
            guard row.issue == nil, let id = row.id, let link = row.link else { return nil }
            return MobileProfile(id: id, name: row.name, link: link)
        } ?? []
        guard !profiles.isEmpty else { throw Issue.profiles }
        var seen = Set<String>()
        return profiles.filter { seen.insert($0.id).inserted }
    }

    private func token() async throws -> String {
        guard let auth else { throw Issue.account }
        let token = try await account.freshToken(auth)
        try saveAuth(auth)
        return token
    }

    private func saveAuth(_ state: OIDAuthState) throws {
        let bytes = try NSKeyedArchiver.archivedData(withRootObject: state, requiringSecureCoding: true)
        try AccessStorage.write("account", bytes)
    }

    private func presentSnapshot() {
        profiles = snapshot.profiles
        selected = profiles.first(where: { $0.id == snapshot.selected })
        canResume = snapshot.pending != nil
    }

    private func store(_ next: Snapshot) throws {
        try AccessStorage.write("profiles", JSONEncoder().encode(next))
        snapshot = next
        presentSnapshot()
    }

    private func verify() async {
        guard let endpoint = URL(string: "https://www.gstatic.com/generate_204") else { return }
        let config = URLSessionConfiguration.ephemeral
        config.timeoutIntervalForRequest = 8
        config.timeoutIntervalForResource = 8
        config.urlCache = nil
        let session = URLSession(configuration: config)
        defer { session.invalidateAndCancel() }
        while !Task.isCancelled && manager?.connection.status == .connected {
            transition(.checking)
            var request = URLRequest(url: endpoint, cachePolicy: .reloadIgnoringLocalCacheData)
            request.httpMethod = "HEAD"
            let response = try? await session.data(for: request).1 as? HTTPURLResponse
            guard !Task.isCancelled, manager?.connection.status == .connected else { return }
            transition(response?.statusCode == 204 && response?.url == endpoint ? .connected : .unconfirmed)
            try? await Task.sleep(nanoseconds: 30_000_000_000)
        }
    }

    private func transition(_ next: Phase) {
        guard next != phase else { return }
        phase = next
        record(next.rawValue)
    }

    private func record(_ event: String) {
        evidence.append("\(Date.now.ISO8601Format()) \(event)")
        if evidence.count > 12 { evidence.removeFirst() }
    }

    private func show(_ problem: Issue) {
        switch problem {
        case .config: issue = "This build needs account settings."
        case .account: issue = "Sign in again with a verified email address."
        case .code: issue = "Check the access code and try again."
        case .used: issue = "This access code is unavailable. Ask for a new one."
        case .network: issue = "Could not reach O Tunnel. Try again."
        case .profiles: issue = "No compatible profiles were found."
        case .storage: issue = "Could not open or save access on this phone."
        case .connect: issue = "Could not connect. Try another profile."
        }
        record(String(describing: problem))
    }

    private func perform(_ action: () async throws -> Void) async {
        guard !busy else { return }
        busy = true
        issue = nil
        defer { busy = false }
        do { try await action() }
        catch let failure as Issue { show(failure) }
        catch { show(.network) }
    }
}
