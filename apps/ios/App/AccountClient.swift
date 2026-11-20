import AppAuth
import Foundation
import UIKit
import Security

struct Setup: Codable {
    let code: String
    let receipt: String

    static func parse(_ value: String, origin: URL) throws -> Setup {
        guard value.utf8.count <= 512, value.hasPrefix("OT1:"),
              let separator = value.firstIndex(of: "#"),
              let submitted = URLComponents(string: String(value[value.index(value.startIndex, offsetBy: 4)..<separator])),
              let expected = URLComponents(url: origin, resolvingAgainstBaseURL: false),
              submitted.scheme == "https", submitted.user == nil, submitted.password == nil,
              submitted.host?.lowercased() == expected.host?.lowercased(),
              (submitted.port ?? 443) == (expected.port ?? 443),
              submitted.path.isEmpty || submitted.path == "/", submitted.query == nil else { throw Issue.code }
        let code = String(value[value.index(after: separator)...])
        let base64 = code.replacingOccurrences(of: "-", with: "+").replacingOccurrences(of: "_", with: "/")
        guard code.count == 43, let decoded = Data(base64Encoded: base64 + "="),
              urlToken(decoded) == code else { throw Issue.code }
        var random = [UInt8](repeating: 0, count: 32)
        guard SecRandomCopyBytes(kSecRandomDefault, random.count, &random) == errSecSuccess else { throw Issue.storage }
        return Setup(code: code, receipt: urlToken(Data(random)))
    }

    private static func urlToken(_ data: Data) -> String {
        data.base64EncodedString().replacingOccurrences(of: "+", with: "-")
            .replacingOccurrences(of: "/", with: "_").replacingOccurrences(of: "=", with: "")
    }
}

enum Issue: Error {
    case config, account, code, used, network, profiles, storage, connect
}

private final class NoRedirects: NSObject, URLSessionTaskDelegate {
    func urlSession(_ session: URLSession, task: URLSessionTask,
                    willPerformHTTPRedirection response: HTTPURLResponse,
                    newRequest request: URLRequest, completionHandler: @escaping (URLRequest?) -> Void) {
        completionHandler(nil)
    }
}

@MainActor
final class AccountClient {
    private var flow: OIDExternalUserAgentSession?
    private let http: URLSession = {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.timeoutIntervalForRequest = 12
        configuration.timeoutIntervalForResource = 15
        configuration.urlCache = nil
        configuration.httpShouldSetCookies = false
        return URLSession(configuration: configuration, delegate: NoRedirects(), delegateQueue: nil)
    }()

    private let issuer = AccountClient.setting("OVPNAccountIssuer")
    private let client = Bundle.main.object(forInfoDictionaryKey: "OVPNAccountClient") as? String ?? ""
    let origin = AccountClient.setting("OVPNAccountOrigin")

    var configured: Bool { issuer != nil && origin != nil && !client.isEmpty }

    private static func setting(_ key: String) -> URL? {
        guard let raw = Bundle.main.object(forInfoDictionaryKey: key) as? String,
              let parts = URLComponents(string: raw), parts.scheme == "https",
              let host = parts.host, !host.isEmpty,
              parts.user == nil, parts.password == nil, parts.query == nil, parts.fragment == nil else { return nil }
        if key == "OVPNAccountOrigin", !parts.path.isEmpty && parts.path != "/" { return nil }
        return parts.url
    }

    func signIn() async throws -> OIDAuthState {
        guard let issuer, configured else { throw Issue.config }
        let configuration: OIDServiceConfiguration = try await withCheckedThrowingContinuation { result in
            OIDAuthorizationService.discoverConfiguration(forIssuer: issuer) { config, _ in
                if let config { result.resume(returning: config) }
                else { result.resume(throwing: Issue.account) }
            }
        }
        guard let redirect = URL(string: "io.github.agentram.ovpn.tunnel:/oauth"),
              let scene = UIApplication.shared.connectedScenes.first(where: { $0.activationState == .foregroundActive }) as? UIWindowScene,
              var presenter = scene.windows.first(where: { $0.isKeyWindow })?.rootViewController,
              configuration.discoveryDocument?.issuer == issuer,
              configuration.authorizationEndpoint.scheme == "https",
              configuration.tokenEndpoint.scheme == "https" else { throw Issue.account }
        while let next = presenter.presentedViewController { presenter = next }
        let request = OIDAuthorizationRequest(configuration: configuration, clientId: client, clientSecret: nil,
                                              scopes: ["openid", "email", "profile"], redirectURL: redirect,
                                              responseType: OIDResponseTypeCode, additionalParameters: ["prompt": "login"])
        defer { flow = nil }
        return try await withCheckedThrowingContinuation { result in
            flow = OIDAuthState.authState(byPresenting: request, presenting: presenter) { state, _ in
                if let state, state.isAuthorized { result.resume(returning: state) }
                else { result.resume(throwing: Issue.account) }
            }
        }
    }

    func resume(_ url: URL) { _ = flow?.resumeExternalUserAgentFlow(with: url) }

    func freshToken(_ state: OIDAuthState) async throws -> String {
        try await withCheckedThrowingContinuation { result in
            state.performAction { _, idToken, error in
                if let idToken, error == nil { result.resume(returning: idToken) }
                else { result.resume(throwing: Issue.account) }
            }
        }
    }

    func verify(_ token: String) async throws {
        _ = try await exchange("/account", token: token, expected: 204)
    }

    func activate(_ setup: Setup, token: String) async throws {
        let body = try JSONEncoder().encode(["code": setup.code, "session": setup.receipt])
        _ = try await exchange("/activate", token: token, body: body, expected: 204)
    }

    func profiles(token: String) async throws -> String {
        let body = try await exchange("/account/profiles", token: token, expected: 200)
        guard let text = String(data: body, encoding: .utf8) else { throw Issue.profiles }
        return text
    }

    private func exchange(_ path: String, token: String, body: Data? = nil, expected: Int) async throws -> Data {
        guard let origin, var parts = URLComponents(url: origin, resolvingAgainstBaseURL: false) else { throw Issue.config }
        parts.path = path
        guard let url = parts.url else { throw Issue.config }
        var request = URLRequest(url: url, cachePolicy: .reloadIgnoringLocalCacheData)
        request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        if let body {
            request.httpMethod = "POST"
            request.httpBody = body
            request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        }
        let (bytes, response) = try await http.bytes(for: request)
        guard let response = response as? HTTPURLResponse else { throw Issue.network }
        switch response.statusCode {
        case expected: break
        case 401: throw Issue.account
        case 404, 410: throw Issue.used
        default: throw Issue.network
        }
        var data = Data()
        for try await byte in bytes {
            guard data.count < (1 << 20) else { throw Issue.profiles }
            data.append(byte)
        }
        return data
    }
}
