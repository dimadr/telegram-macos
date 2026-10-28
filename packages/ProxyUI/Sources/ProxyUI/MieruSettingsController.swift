import Cocoa
import TGUIKit
import SwiftSignalKit
import TelegramTheme
import GeneralRows
import InputDataUI
import MieruTransport

func showMieruError(_ error: Error) {
    let alert = NSAlert()
    alert.messageText = "Mieru" //TODOLANG
    alert.informativeText = mieruErrorText(error)
    alert.runModal()
}

private func mieruErrorText(_ error: Error) -> String {
    if let diagnostic = error as? MieruDiagnostic { return diagnostic.localizedDescription }
    return (error as? MieruError ?? .invalidConfiguration).message
}

private struct MieruSettingsState: Equatable {
    var profiles = MieruProfileCollection()
    var snapshot = MieruRuntime.shared.snapshot
    var result = ""
    var checking = false
}

private final class MieruSettingsArguments {
    let add: () -> Void
    let edit: (MieruProfile) -> Void
    let select: (UUID) -> Void
    let importFile: () -> Void
    let importLink: () -> Void
    let test: () -> Void
    let cancel: () -> Void
    init(add: @escaping () -> Void, edit: @escaping (MieruProfile) -> Void, select: @escaping (UUID) -> Void, importFile: @escaping () -> Void, importLink: @escaping () -> Void, test: @escaping () -> Void, cancel: @escaping () -> Void) {
        self.add = add; self.edit = edit; self.select = select; self.importFile = importFile
        self.importLink = importLink; self.test = test; self.cancel = cancel
    }
}

private func mieruSettingsEntries(_ state: MieruSettingsState, _ arguments: MieruSettingsArguments) -> [InputDataEntry] {
    var entries: [InputDataEntry] = [.sectionId(0, type: .normal)]
    var index: Int32 = 0
    func row(_ id: String, _ name: String, _ type: GeneralInteractedType = .none, enabled: Bool = true, action: @escaping () -> Void) {
        entries.append(.general(sectionId: 1, index: index, value: .none, error: nil, identifier: InputDataIdentifier(id), data: InputDataGeneralData(name: name, color: theme.colors.text, type: type, viewType: .singleItem, enabled: enabled, action: action)))
        index += 1
    }
    row("mieru_enabled", "Enable Mieru", .switchable(state.snapshot.enabled)) { //TODOLANG
        do { try MieruRuntime.shared.setEnabled(!state.snapshot.enabled) } catch { showMieruError(error) }
    }
    entries.append(.desc(sectionId: 1, index: index, text: .plain(state.snapshot.status), data: InputDataGeneralTextData(viewType: .textBottomItem)))
    index += 1
    row("mieru_add", "Add profile", .next, action: arguments.add) //TODOLANG
    for profile in state.profiles.profiles {
        row("mieru_select_\(profile.id)", profile.name, .selectable(profile.id == state.profiles.activeID)) { arguments.select(profile.id) }
        row("mieru_edit_\(profile.id)", "Edit \(profile.name)", .next) { arguments.edit(profile) } //TODOLANG
    }
    row("mieru_import_file", "Import profile from file…", .next, action: arguments.importFile) //TODOLANG
    row("mieru_import_link", "Import Mieru link…", .next, action: arguments.importLink) //TODOLANG
    row("mieru_test", "Check connection", state.checking ? .loading : .none, enabled: state.profiles.active != nil && !state.checking, action: arguments.test) //TODOLANG
    if state.checking { row("mieru_cancel", "Cancel check", action: arguments.cancel) } //TODOLANG
    if !state.result.isEmpty {
        entries.append(.desc(sectionId: 1, index: index, text: .plain(state.result), data: InputDataGeneralTextData(viewType: .textBottomItem)))
        index += 1
    }
    entries.append(.desc(sectionId: 1, index: index, text: .plain("Restart Telegramita after changing the active profile or enabling/disabling Mieru. Connection checks use a separate client and a Telegram DC at TCP port 443, with a 10-second timeout and STANDARD handshake. Calls, sharing and unsupported transports are unavailable while Mieru is active."), data: InputDataGeneralTextData(viewType: .textBottomItem))) //TODOLANG
    entries.append(.sectionId(2, type: .normal))
    return entries
}

func mieruSettingsController(pushController: @escaping (ViewController) -> Void) -> InputDataController {
    let stateValue = Atomic(value: MieruSettingsState())
    let statePromise = ValuePromise(MieruSettingsState(), ignoreRepeated: true)
    let updateState: (@escaping (MieruSettingsState) -> MieruSettingsState) -> Void = { f in statePromise.set(stateValue.modify(f)) }
    var check: MieruConnectionCheck?
    let cancelCheck = {
        check?.cancel(); check = nil
        updateState { var state = $0; state.checking = false; state.result = "Connection check cancelled"; return state } //TODOLANG
    }
    let reload = {
        do {
            let profiles = try MieruRuntime.shared.profiles()
            updateState { var state = $0; state.profiles = profiles; state.snapshot = MieruRuntime.shared.snapshot; return state }
        } catch {
            updateState { var state = $0; state.result = mieruErrorText(error); return state }
        }
    }
    reload()
    let observer = NotificationCenter.default.addObserver(forName: MieruRuntime.changed, object: nil, queue: .main) { _ in
        let before = stateValue.with { $0.profiles }
        reload()
        if before != stateValue.with({ $0.profiles }), check != nil { cancelCheck() }
    }
    let arguments = MieruSettingsArguments(add: {
        pushController(mieruProfileController(profile: nil))
    }, edit: { profile in
        pushController(mieruProfileController(profile: profile))
    }, select: { id in
        do { try MieruRuntime.shared.selectProfile(id) } catch { showMieruError(error) }
    }, importFile: {
        let panel = NSOpenPanel()
        panel.allowedFileTypes = ["json", "yaml", "yml", "txt"]
        panel.allowsMultipleSelection = false
        panel.begin { result in
            guard result == .OK, let url = panel.url else { return }
            do {
                let size = try url.resourceValues(forKeys: [.fileSizeKey]).fileSize ?? 0
                guard size > 0, size <= 1024 * 1024 else {
                    throw MieruDiagnostic(stage: "configuration", code: "size", message: "Profile file must be between 1 byte and 1 MiB") //TODOLANG
                }
                try MieruRuntime.shared.importProfile(Data(contentsOf: url))
            } catch { showMieruError(error) }
        }
    }, importLink: {
        pushController(mieruLinkImportController())
    }, test: {
        guard let profile = stateValue.with({ $0.profiles.active }) else { return }
        check?.cancel()
        updateState { var state = $0; state.checking = true; state.result = "Checking connection…"; return state } //TODOLANG
        check = MieruConfiguration.probe(profile) { result in
            check = nil
            updateState { current in
                var state = current; state.checking = false
                switch result {
                case let .success(value): state.result = value.summary
                case let .failure(error): state.result = mieruErrorText(error)
                }
                return state
            }
        }
    }, cancel: cancelCheck)
    let signal = combineLatest(statePromise.get(), appearanceSignal) |> deliverOnPrepareQueue |> map { state, _ in
        InputDataSignalValue(entries: mieruSettingsEntries(state, arguments))
    }
    let controller = InputDataController(dataSignal: signal, title: "Mieru", hasDone: false) //TODOLANG
    controller.onDeinit = { NotificationCenter.default.removeObserver(observer); check?.cancel(); check = nil }
    return controller
}

private let mieruName = InputDataIdentifier("mieru_name")
private let mieruServer = InputDataIdentifier("mieru_server")
private let mieruPorts = InputDataIdentifier("mieru_ports")
private let mieruTransport = InputDataIdentifier("mieru_transport")
private let mieruUsername = InputDataIdentifier("mieru_username")
private let mieruPassword = InputDataIdentifier("mieru_password")
private let mieruMTU = InputDataIdentifier("mieru_mtu")
private let mieruMultiplexing = InputDataIdentifier("mieru_multiplexing")
private let mieruHandshake = InputDataIdentifier("mieru_handshake")

private struct MieruEditorState: Equatable {
    var fields: MieruProfileFields
    var showPassword = false
}

private func mieruEditorEntries(_ state: MieruEditorState, togglePassword: @escaping () -> Void) -> [InputDataEntry] {
    var entries: [InputDataEntry] = [.sectionId(0, type: .normal)]
    var index: Int32 = 0
    func input(_ id: InputDataIdentifier, _ name: String, _ value: String, _ hint: String, secure: Bool = false, limit: Int32 = 255) {
        entries.append(.input(sectionId: 1, index: index, value: .string(value), error: nil, identifier: id, mode: secure ? .secure : .plain, data: InputDataRowData(viewType: .singleItem), placeholder: InputDataInputPlaceholder(name), inputPlaceholder: hint, filter: { $0 }, limit: limit))
        index += 1
    }
    func selector(_ id: InputDataIdentifier, _ name: String, _ value: String, _ values: [String]) {
        entries.append(.selector(sectionId: 1, index: index, value: .string(value), error: nil, identifier: id, placeholder: name, viewType: .singleItem, values: values.map { ValuesSelectorValue(localized: $0, value: InputDataValue.string($0)) }))
        index += 1
    }
    input(mieruName, "Profile name", state.fields.name, "Mieru") //TODOLANG
    input(mieruServer, "Server", state.fields.server, "IPv4, IPv6 or domain") //TODOLANG
    input(mieruPorts, "Port", state.fields.ports, "443 or 4012-4021", limit: 11) //TODOLANG
    selector(mieruTransport, "Protocol", state.fields.transport, ["TCP", "UDP"]) //TODOLANG
    input(mieruUsername, "Username", state.fields.username, "Required", limit: 64) //TODOLANG
    input(mieruPassword, "Password", state.fields.password, "Required (imported hash is preserved)", secure: !state.showPassword, limit: 64) //TODOLANG
    entries.append(.general(sectionId: 1, index: index, value: .none, error: nil, identifier: InputDataIdentifier("mieru_show_password"), data: InputDataGeneralData(name: "Show password", color: theme.colors.text, type: .switchable(state.showPassword), viewType: .singleItem, action: togglePassword))) //TODOLANG
    index += 1
    input(mieruMTU, "MTU", state.fields.mtu, "0 = Automatic", limit: 4) //TODOLANG
    selector(mieruMultiplexing, "Multiplexing", state.fields.multiplexing, ["OFF", "LOW", "MIDDLE", "HIGH"]) //TODOLANG
    selector(mieruHandshake, "Handshake", state.fields.handshake, ["STANDARD", "NO_WAIT"]) //TODOLANG
    entries.append(.general(sectionId: 1, index: index, value: .none, error: nil, identifier: InputDataIdentifier("mieru_save"), data: InputDataGeneralData(name: "Save", color: theme.colors.accent, viewType: .singleItem))) //TODOLANG
    index += 1
    entries.append(.desc(sectionId: 1, index: index, text: .plain("Saved passwords are kept in Keychain. For imported profiles with several servers or ports, these fields edit the first entry; other entries and traffic-pattern settings are preserved."), data: InputDataGeneralTextData(viewType: .textBottomItem))) //TODOLANG
    entries.append(.sectionId(2, type: .normal))
    return entries
}

private func mieruProfileController(profile: MieruProfile?) -> InputDataController {
    let fields: MieruProfileFields
    do { fields = try profile.map { try MieruProfileFields(profile: $0) } ?? MieruProfileFields() }
    catch { showMieruError(error); return InputDataController(dataSignal: .single(InputDataSignalValue(entries: [])), title: "Mieru") }
    let initial = MieruEditorState(fields: fields)
    let stateValue = Atomic(value: initial)
    let statePromise = ValuePromise(initial, ignoreRepeated: true)
    let updateState: (@escaping (MieruEditorState) -> MieruEditorState) -> Void = { f in statePromise.set(stateValue.modify(f)) }
    let signal = combineLatest(statePromise.get(), appearanceSignal) |> deliverOnPrepareQueue |> map { state, _ in
        InputDataSignalValue(entries: mieruEditorEntries(state, togglePassword: { updateState { var value = $0; value.showPassword.toggle(); return value } }))
    }
    return InputDataController(dataSignal: signal, title: profile == nil ? "Add Mieru profile" : "Edit Mieru profile", validateData: { _ in //TODOLANG
        do {
            let fields = stateValue.with { $0.fields }
            let value = try fields.profile(replacing: profile)
            try MieruRuntime.shared.saveProfile(value)
            return .success(.navigationBack)
        } catch { return .fail(.alert(mieruErrorText(error))) }
    }, updateDatas: { data in
        updateState { current in
            var state = current
            state.fields.name = data[mieruName]?.stringValue ?? state.fields.name
            state.fields.server = data[mieruServer]?.stringValue ?? state.fields.server
            state.fields.ports = data[mieruPorts]?.stringValue ?? state.fields.ports
            state.fields.transport = data[mieruTransport]?.stringValue ?? state.fields.transport
            state.fields.username = data[mieruUsername]?.stringValue ?? state.fields.username
            state.fields.password = data[mieruPassword]?.stringValue ?? state.fields.password
            state.fields.mtu = data[mieruMTU]?.stringValue ?? state.fields.mtu
            state.fields.multiplexing = data[mieruMultiplexing]?.stringValue ?? state.fields.multiplexing
            state.fields.handshake = data[mieruHandshake]?.stringValue ?? state.fields.handshake
            return state
        }
        return .fail(.none)
    }, hasDone: false)
}

private func mieruLinkImportController() -> InputDataController {
    let identifier = InputDataIdentifier("mieru_link")
    let entries: [InputDataEntry] = [.sectionId(0, type: .normal),
        .input(sectionId: 1, index: 0, value: .string(""), error: nil, identifier: identifier, mode: .secure, data: InputDataRowData(viewType: .singleItem), placeholder: nil, inputPlaceholder: "mieru:// or mierus://", filter: { $0 }, limit: 1024 * 1024)]
    return InputDataController(dataSignal: .single(InputDataSignalValue(entries: entries)), title: "Import Mieru link", validateData: { data in //TODOLANG
        let text = (data[identifier]?.stringValue ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
        guard text.hasPrefix("mieru://") || text.hasPrefix("mierus://") else { return .fail(.alert("Paste an official mieru:// or mierus:// link")) } //TODOLANG
        do { try MieruRuntime.shared.importProfile(Data(text.utf8)); return .success(.navigationBack) }
        catch { return .fail(.alert(mieruErrorText(error))) }
    }, hasDone: true)
}
