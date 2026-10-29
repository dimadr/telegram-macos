import Foundation
import Security
import MieruABI
import Darwin

public enum MieruError: Int, Error, CaseIterable {
    case invalidConfiguration = -1
    case closed = -2
    case timeout = -3
    case network = -4
    case keychain = -5
    case unsupported = -6

    public var message: String {
        switch self {
        case .invalidConfiguration: return "Invalid Mieru configuration"
        case .closed: return "Mieru connection closed"
        case .timeout: return "Mieru connection timed out"
        case .network: return "Mieru connection failed"
        case .keychain: return "Mieru configuration is unavailable in Keychain"
        case .unsupported: return "This transport is disabled while Mieru is active"
        }
    }

    static func from(_ code: Int64) -> MieruError {
        return MieruError(rawValue: Int(code)) ?? .network
    }
}

public struct MieruSnapshot: Equatable {
    public let enabled: Bool
    public let active: Bool
    public let status: String
}

public final class MieruRuntime {
    public static let shared = MieruRuntime()
    public static let changed = Notification.Name("Telegramita.Mieru.changed")
    private static let policyChanged = Notification.Name("Telegramita.Mieru.policyChanged")
    public let enabled: Bool
    private let defaults: UserDefaults
    private let lock = NSLock()
    private var client: UInt64 = 0
    private var status: String
    private var profileChanged = false
    private var startupError: MieruError?
    private var transportDiagnostic: MieruDiagnostic?
    private var extensionObserver: NSObjectProtocol?
    private let service = "Telegramita.Mieru.profile.v1"
    private let repository = MieruProfileRepository()
    private let profileLock = NSRecursiveLock()

    private init() {
        let defaults = UserDefaults(suiteName: "org.dimadr.Telegramita.mieru")!
        let isExtension = Bundle.main.bundlePath.hasSuffix(".appex")
        let enabled = defaults.bool(forKey: isExtension ? "Telegramita.Mieru.active" : "Telegramita.Mieru.enabled")
        self.defaults = defaults
        self.enabled = enabled
        self.status = enabled ? "Starting Mieru" : "Mieru is off"
        if isExtension {
            self.extensionObserver = DistributedNotificationCenter.default().addObserver(forName: Self.policyChanged, object: nil, queue: nil) { [weak self] _ in
                guard let self = self else { return }
                self.defaults.synchronize()
                if self.defaults.bool(forKey: "Telegramita.Mieru.active") { Darwin.exit(0) }
            }
        } else {
            defaults.set(enabled, forKey: "Telegramita.Mieru.active")
            guard defaults.synchronize() else {
                self.status = "Cannot save the shared Mieru routing policy"
                return
            }
            DistributedNotificationCenter.default().postNotificationName(Self.policyChanged, object: nil, userInfo: nil, deliverImmediately: true)
        }
        guard self.enabled else { return }
        guard !isExtension else {
            self.status = "Sharing is disabled while Mieru is active"
            return
        }
        DispatchQueue.global(qos: .userInitiated).async { self.startClient() }
    }

    private func startClient() {
        do {
            let data = try self.loadProfile()
            let handle = MieruClientNew()
            guard handle != 0 else { throw MieruError.invalidConfiguration }
            let stored = data.withUnsafeBytes { bytes in
                MieruClientStore(handle, bytes.baseAddress?.assumingMemoryBound(to: CChar.self), Int64(bytes.count))
            }
            guard stored == 0 else {
                let diagnostic = self.clientDiagnostic(handle)
                MieruClientFree(handle)
                throw diagnostic ?? MieruDiagnostic(stage: "configuration", code: "rejected", message: MieruError.from(Int64(stored)).message)
            }
            let started = MieruClientStart(handle)
            guard started == 0 else {
                let diagnostic = self.clientDiagnostic(handle)
                MieruClientFree(handle)
                throw diagnostic ?? MieruDiagnostic(stage: "client_start", code: "start_failed", message: MieruError.from(Int64(started)).message)
            }
            self.lock.lock()
            self.client = handle
            self.lock.unlock()
            self.report("Mieru started; waiting for network traffic")
        } catch {
            let routingError = error as? MieruError ?? .network
            self.lock.lock()
            self.startupError = routingError
            self.transportDiagnostic = error as? MieruDiagnostic
            self.lock.unlock()
            self.report((error as? MieruDiagnostic)?.localizedDescription ?? routingError.message)
        }
    }

    public var snapshot: MieruSnapshot {
        self.lock.lock()
        defer { self.lock.unlock() }
        let configured = self.defaults.bool(forKey: "Telegramita.Mieru.enabled")
        let suffix = configured != self.enabled || self.profileChanged ? "; restart the app to apply" : ""
        return MieruSnapshot(enabled: configured, active: self.enabled, status: self.status + suffix)
    }

    public func setEnabled(_ value: Bool) throws {
        if value { _ = try self.loadProfile() }
        self.defaults.set(value, forKey: "Telegramita.Mieru.enabled")
        guard self.defaults.synchronize() else {
            throw MieruDiagnostic(stage: "preferences", code: "save", message: "Cannot save the Mieru enable setting")
        }
        self.notify()
    }

    public func importProfile(_ data: Data) throws {
        let imported = try MieruConfiguration.normalize(data)
        self.profileLock.lock()
        defer { self.profileLock.unlock() }
        var collection = try self.profiles()
        var selected: UUID?
        for profile in imported.profiles {
            var entry = profile
            var suffix = 2
            while collection.profiles.contains(where: { $0.name == entry.name }) {
                var object = try profile.object()
                object["profileName"] = "\(profile.name) (\(suffix))"
                entry = MieruProfile(id: profile.id, configuration: try JSONSerialization.data(withJSONObject: object))
                suffix += 1
            }
            collection.profiles.append(entry)
            if imported.activeID == profile.id { selected = entry.id }
        }
        collection.activeID = selected ?? collection.activeID
        try self.repository.write(collection)
        self.configurationSaved()
    }

    public func profiles() throws -> MieruProfileCollection {
        self.profileLock.lock()
        defer { self.profileLock.unlock() }
        let saved = try self.repository.read()
        if !saved.profiles.isEmpty { return saved }
        if let legacy = try self.loadLegacyProfile() {
            let imported = try MieruConfiguration.normalize(legacy)
            try self.repository.write(imported)
            return imported
        }
        return saved
    }

    public func saveProfile(_ profile: MieruProfile) throws {
        self.profileLock.lock()
        defer { self.profileLock.unlock() }
        var collection = try self.profiles()
        if let index = collection.profiles.firstIndex(where: { $0.id == profile.id }) { collection.profiles[index] = profile }
        else { collection.profiles.append(profile) }
        collection.activeID = profile.id
        try self.repository.write(collection)
        self.configurationSaved()
    }

    public func selectProfile(_ id: UUID) throws {
        self.profileLock.lock()
        defer { self.profileLock.unlock() }
        var collection = try self.profiles()
        guard collection.profiles.contains(where: { $0.id == id }) else { throw MieruError.invalidConfiguration }
        collection.activeID = id
        try self.repository.write(collection)
        self.configurationSaved()
    }

    private func configurationSaved() {
        self.lock.lock()
        self.profileChanged = true
        self.lock.unlock()
        self.report("Profile saved in Keychain; restart Telegramita to apply")
    }

    private func clientDiagnostic(_ handle: UInt64) -> MieruDiagnostic? {
        var output = Data(count: 4096)
        let count = output.withUnsafeMutableBytes { MieruClientDiagnostic(handle, $0.baseAddress, Int64($0.count)) }
        guard count > 0, count <= Int64(output.count) else { return nil }
        output.count = Int(count)
        return try? JSONDecoder().decode(MieruDiagnostic.self, from: output)
    }

    public func makeSocket(queue: DispatchQueue) throws -> MieruSocket {
        self.lock.lock()
        let client = self.client
        let error = self.startupError ?? .closed
        self.lock.unlock()
        guard self.enabled, client != 0 else { throw error }
        let connection = MieruConnectionNew(client)
        guard connection != 0 else { throw MieruError.closed }
        return MieruSocket(handle: connection, queue: queue)
    }

    public func report(_ status: String) {
        self.lock.lock()
        let isGenericError = MieruError.allCases.contains(where: { $0.message == status })
        let resolved = isGenericError ? self.transportDiagnostic?.localizedDescription ?? status : status
        if !isGenericError && status != self.transportDiagnostic?.localizedDescription { self.transportDiagnostic = nil }
        let changed = self.status != resolved
        self.status = resolved
        self.lock.unlock()
        if changed { self.notify() }
    }

    func reportConnectionFailure(_ handle: UInt64) {
        var output = Data(count: 4096)
        let count = output.withUnsafeMutableBytes { MieruConnectionDiagnostic(handle, $0.baseAddress, Int64($0.count)) }
        guard count > 0, count <= Int64(output.count) else { return }
        output.count = Int(count)
        guard let diagnostic = try? JSONDecoder().decode(MieruDiagnostic.self, from: output), !diagnostic.code.isEmpty else { return }
        self.lock.lock()
        self.transportDiagnostic = diagnostic
        self.status = diagnostic.localizedDescription
        self.lock.unlock()
        self.notify()
    }

    private func notify() {
        DispatchQueue.main.async { NotificationCenter.default.post(name: Self.changed, object: nil) }
    }

    private func keychainQuery() -> [String: Any] {
        return [kSecClass as String: kSecClassGenericPassword, kSecAttrService as String: self.service,
                kSecAttrAccount as String: "active", kSecAttrSynchronizable as String: false]
    }

    private func loadProfile() throws -> Data {
        guard let profile = try self.profiles().active else {
            throw MieruDiagnostic(stage: "configuration", code: "missing_profile", message: "Add and save a Mieru profile first")
        }
        return profile.configuration
    }

    private func loadLegacyProfile() throws -> Data? {
        var query = self.keychainQuery()
        query[kSecReturnData as String] = true
        query[kSecMatchLimit as String] = kSecMatchLimitOne
        var result: CFTypeRef?
        let status = SecItemCopyMatching(query as CFDictionary, &result)
        if status == errSecItemNotFound { return nil }
        guard status == errSecSuccess, let data = result as? Data else {
            throw MieruDiagnostic(stage: "keychain", code: "osstatus_\(status)", message: "Cannot read the previous Telegramita profile from Keychain (OSStatus \(status)).")
        }
        return data
    }

    deinit {
        if let observer = self.extensionObserver { DistributedNotificationCenter.default().removeObserver(observer) }
        if self.client != 0 { MieruClientFree(self.client) }
    }
}
