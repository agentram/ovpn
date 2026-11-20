import NetworkExtension
import SwiftUI

@main
struct OTunnelApp: App {
    @StateObject private var model = TunnelModel()
    @AppStorage("language") private var language = "en"

    var body: some Scene {
        WindowGroup {
            Home(model: model, language: $language)
                .environment(\.locale, Locale(identifier: language))
                .task { await model.load() }
                .onOpenURL { model.account.resume($0) }
                .onReceive(NotificationCenter.default.publisher(for: .NEVPNStatusDidChange)) { _ in
                    model.statusChanged()
                }
        }
    }
}

private struct Home: View {
    @ObservedObject var model: TunnelModel
    @Binding var language: String
    @State private var sheet: Panel?

    private enum Panel: String, Identifiable {
        case settings, profiles, report, access
        var id: String { rawValue }
    }

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(spacing: 20) {
                    if !model.signedIn { signIn }
                    else if model.profiles.isEmpty { activate }
                    else { connection }
                    if let issue = model.issue {
                        Label(LocalizedStringKey(issue), systemImage: "exclamationmark.circle")
                            .foregroundStyle(Color.red).frame(maxWidth: .infinity, alignment: .leading)
                    }
                    if model.busy { ProgressView().frame(maxWidth: .infinity) }
                    if model.signedIn {
                        Button { sheet = .report } label: { Label("Problem report", systemImage: "doc.text") }
                            .buttonStyle(.borderless).padding(.vertical, 8)
                    }
                }
                .frame(maxWidth: 500)
                .padding(20)
                .frame(maxWidth: .infinity)
            }
            .background(Color(uiColor: .systemGroupedBackground))
            .navigationTitle("O Tunnel")
            .toolbar {
                ToolbarItem(placement: .navigationBarTrailing) {
                    Button { sheet = .settings } label: {
                        Image(systemName: "gearshape").accessibilityLabel("Settings")
                    }
                }
            }
            .sheet(item: $sheet) { choice in
                NavigationStack {
                    Group {
                        switch choice {
                        case .profiles: profileList
                        case .report: diagnostics
                        case .settings: settings
                        case .access:
                            ScrollView { activate.padding(20) }.navigationTitle("Set up access")
                        }
                    }
                    .toolbar { ToolbarItem(placement: .confirmationAction) {
                        Button("Done") { sheet = nil }
                    } }
                }
                .environment(\.locale, Locale(identifier: language))
            }
        }
        .tint(Color(uiColor: UIColor { traits in
            traits.userInterfaceStyle == .dark
                ? UIColor(red: 0.36, green: 0.90, blue: 0.69, alpha: 1)
                : UIColor(red: 0.04, green: 0.37, blue: 0.26, alpha: 1)
        }))
    }

    private var signIn: some View {
        card {
            Text("Welcome to O Tunnel").font(.title2.bold())
            Text("Sign in or create an account with email or Google.").foregroundStyle(.secondary)
            primary("Sign in", icon: "person.crop.circle") { await model.signIn() }
        }
    }

    private var activate: some View {
        card {
            Text("Set up access").font(.title2.bold())
            Text("Paste the access code you received.").foregroundStyle(.secondary)
            SecureField("Access code", text: $model.code)
                .textContentType(.oneTimeCode)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .padding(12)
                .background(Color(uiColor: .tertiarySystemFill), in: RoundedRectangle(cornerRadius: 12))
            primary("Continue", icon: "arrow.right",
                    disabled: model.code.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty && !model.canResume) {
                await model.activate()
            }
        }
    }

    private var connection: some View {
        card {
            VStack(spacing: 14) {
                ZStack {
                    Circle().fill(signal.opacity(0.11))
                    Circle().strokeBorder(signal.opacity(0.30), lineWidth: 1).padding(9)
                    Circle().strokeBorder(signal, lineWidth: 2.5).padding(19)
                    Image(systemName: model.phase == .connected ? "checkmark" : "power")
                        .font(.title2.weight(.semibold)).foregroundStyle(signal)
                }
                .frame(width: 92, height: 92)
                .accessibilityHidden(true)
                Text(title).font(.title2.bold()).multilineTextAlignment(.center)
                if let selected = model.selected {
                    Text(selected.name).foregroundStyle(.secondary).multilineTextAlignment(.center)
                }
            }
            .frame(maxWidth: .infinity)
            .padding(.vertical, 16)
            if model.phase == .disconnected {
                primary("Connect", icon: "power", disabled: model.selected == nil) { await model.connect() }
            } else {
                primary("Disconnect", icon: "stop", disabled: model.phase == .disconnecting) { model.stop() }
            }
            Button { sheet = .profiles } label: {
                Label(LocalizedStringKey(model.selected == nil ? "Choose profile" : "Change profile"),
                      systemImage: "arrow.left.arrow.right")
                    .frame(maxWidth: .infinity, minHeight: 46)
            }
            .buttonStyle(.bordered)
        }
    }

    private var profileList: some View {
        List {
            Section {
                ForEach(model.profiles) { profile in
                    Button {
                        Task { await model.select(profile); sheet = nil }
                    } label: {
                        HStack {
                            Text(profile.name)
                            Spacer()
                            if profile == model.selected { Image(systemName: "checkmark.circle.fill") }
                        }
                        .frame(minHeight: 44)
                    }
                    .accessibilityAddTraits(profile == model.selected ? .isSelected : [])
                }
            } footer: {
                Text("Choose any profile and tap Connect. If it will not connect, try another one.")
            }
        }
        .navigationTitle("Connection profiles")
    }

    private var diagnostics: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                Text("Review the connection details before sharing.")
                Text(model.report).font(.system(.footnote, design: .monospaced))
                    .textSelection(.enabled).frame(maxWidth: .infinity, alignment: .leading)
                    .padding(16)
                    .background(Color(uiColor: .secondarySystemGroupedBackground), in: RoundedRectangle(cornerRadius: 15))
                ShareLink(item: model.report) { Label("Share report", systemImage: "square.and.arrow.up") }
                    .buttonStyle(.borderedProminent)
            }
            .padding(20)
        }
        .navigationTitle("Problem report")
    }

    private var settings: some View {
        Form {
            Picker("Language", selection: $language) {
                Text("English").tag("en")
                Text("Русский").tag("ru")
                Text("简体中文").tag("zh-Hans")
            }
            if model.signedIn {
                Button("Enter a new code") { sheet = .access }
                    .disabled(model.busy)
                Button("Update profiles") { Task { await model.refresh(); sheet = nil } }
                    .disabled(model.profiles.isEmpty || model.busy)
                Button("Sign out", role: .destructive) { Task { await model.signOut(); sheet = nil } }
                    .disabled(model.busy)
            }
        }
        .navigationTitle("Settings")
    }

    private var signal: Color {
        switch model.phase {
        case .disconnected: return .secondary
        case .connected: return .green
        case .unconfirmed: return .orange
        default: return .accentColor
        }
    }

    private var title: LocalizedStringKey {
        switch model.phase {
        case .disconnected: return "Disconnected"
        case .connecting: return "Connecting…"
        case .checking: return "Checking connection…"
        case .connected: return "Connected"
        case .unconfirmed: return "Connection unconfirmed"
        case .disconnecting: return "Disconnecting…"
        }
    }

    private func card<Content: View>(@ViewBuilder _ content: () -> Content) -> some View {
        VStack(alignment: .leading, spacing: 14, content: content)
            .padding(20)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(Color(uiColor: .secondarySystemGroupedBackground), in: RoundedRectangle(cornerRadius: 24))
    }

    private func primary(_ title: LocalizedStringKey, icon: String, disabled: Bool = false,
                         _ action: @escaping () async -> Void) -> some View {
        Button { Task { await action() } } label: {
            Label(title, systemImage: icon)
                .font(.headline)
                .frame(maxWidth: .infinity, minHeight: 48)
        }
        .buttonStyle(.borderedProminent)
        .controlSize(.large)
        .disabled(disabled || model.busy)
    }
}
