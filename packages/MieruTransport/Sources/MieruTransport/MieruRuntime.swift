import Foundation
import Security
import MieruABI
import Darwin

public enum MieruError: Int, Error {
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
    private var extensionObserver: NSObjectProtocol?
    private let service = "Telegramita.Mieru.profile.v1"

    private init() {
        let defaults = UserDefaults(suiteName: "6N38VWS5BX.ru.keepcoder.Telegram")!
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
                MieruClientFree(handle)
                throw MieruError.from(Int64(stored))
            }
            let started = MieruClientStart(handle)
            guard started == 0 else {
                MieruClientFree(handle)
                throw MieruError.from(Int64(started))
            }
            self.lock.lock()
            self.client = handle
            self.lock.unlock()
            self.report("Mieru started; waiting for network traffic")
        } catch {
            let error = error as? MieruError ?? .invalidConfiguration
            self.lock.lock()
            self.startupError = error
            self.lock.unlock()
            self.report(error.message)
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
        guard self.defaults.synchronize() else { throw MieruError.keychain }
        self.notify()
    }

    public func importProfile(_ data: Data) throws {
        guard !data.isEmpty, data.count <= 1024 * 1024 else { throw MieruError.invalidConfiguration }
        let validator = MieruClientNew()
        guard validator != 0 else { throw MieruError.invalidConfiguration }
        defer { MieruClientFree(validator) }
        let result = data.withUnsafeBytes { bytes in
            MieruClientStore(validator, bytes.baseAddress?.assumingMemoryBound(to: CChar.self), Int64(bytes.count))
        }
        guard result == 0 else { throw MieruError.from(Int64(result)) }
        let query = self.keychainQuery()
        let attributes: [String: Any] = [kSecValueData as String: data]
        let update = SecItemUpdate(query as CFDictionary, attributes as CFDictionary)
        if update == errSecItemNotFound {
            var item = query
            item[kSecValueData as String] = data
            item[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly
            guard SecItemAdd(item as CFDictionary, nil) == errSecSuccess else { throw MieruError.keychain }
        } else if update != errSecSuccess {
            throw MieruError.keychain
        }
        self.lock.lock()
        self.profileChanged = true
        self.lock.unlock()
        self.report("Configuration saved")
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
        let changed = self.status != status
        self.status = status
        self.lock.unlock()
        if changed { self.notify() }
    }

    private func notify() {
        DispatchQueue.main.async { NotificationCenter.default.post(name: Self.changed, object: nil) }
    }

    private func keychainQuery() -> [String: Any] {
        return [kSecClass as String: kSecClassGenericPassword, kSecAttrService as String: self.service,
                kSecAttrAccount as String: "active", kSecAttrSynchronizable as String: false]
    }

    private func loadProfile() throws -> Data {
        var query = self.keychainQuery()
        query[kSecReturnData as String] = true
        query[kSecMatchLimit as String] = kSecMatchLimitOne
        var result: CFTypeRef?
        guard SecItemCopyMatching(query as CFDictionary, &result) == errSecSuccess, let data = result as? Data else {
            throw MieruError.keychain
        }
        return data
    }

    deinit {
        if let observer = self.extensionObserver { DistributedNotificationCenter.default().removeObserver(observer) }
        if self.client != 0 { MieruClientFree(self.client) }
    }
}
