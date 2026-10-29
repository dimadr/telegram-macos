import Foundation
import Darwin
import MieruABI
import Security

public struct MieruDiagnostic: Error, Codable, LocalizedError {
    public let stage: String
    public let code: String
    public let message: String
    public var errorDescription: String? { "\(stage): \(message)" }
    public init(stage: String, code: String, message: String) {
        self.stage = stage; self.code = code; self.message = message
    }
}

public struct MieruProbeResult: Decodable {
    public let configValid: Bool
    public let clientStarted: Bool
    public let connectionEstablished: Bool
    public let diagnostic: MieruDiagnostic?
    public var summary: String {
        var lines = [configValid ? "CONFIG VALID" : "CONFIG NOT VALID"]
        if clientStarted { lines.append("CLIENT STARTED") }
        if connectionEstablished { lines.append("CONNECTION ESTABLISHED (Mieru to target)") }
        if let diagnostic = diagnostic { lines.append("FAILED - \(diagnostic.stage): \(diagnostic.message)") }
        return lines.joined(separator: "\n")
    }
}

public struct MieruProfile: Codable, Equatable {
    public let id: UUID
    public let configuration: Data
    public var name: String { (try? object()["profileName"] as? String) ?? "Mieru" }
    init(id: UUID = UUID(), configuration: Data) { self.id = id; self.configuration = configuration }
    func object() throws -> [String: Any] {
        guard let value = try JSONSerialization.jsonObject(with: configuration) as? [String: Any] else {
            throw MieruDiagnostic(stage: "configuration", code: "structure", message: "Invalid saved profile structure")
        }
        return value
    }
}

public struct MieruProfileFields: Equatable {
    public var name = "Mieru"
    public var server = ""
    public var ports = ""
    public var transport = "TCP"
    public var username = ""
    public var password = ""
    public var mtu = "0"
    public var multiplexing = "LOW"
    public var handshake = "STANDARD"
    public init() {}
    public init(profile: MieruProfile) throws {
        let object = try profile.object()
        name = object["profileName"] as? String ?? ""
        let user = object["user"] as? [String: Any] ?? [:]
        username = user["name"] as? String ?? ""
        password = user["password"] as? String ?? ""
        let endpoint = (object["servers"] as? [[String: Any]])?.first ?? [:]
        server = endpoint["ipAddress"] as? String ?? endpoint["domainName"] as? String ?? ""
        let binding = (endpoint["portBindings"] as? [[String: Any]])?.first ?? [:]
        ports = binding["portRange"] as? String ?? (binding["port"] as? NSNumber)?.stringValue ?? ""
        transport = binding["protocol"] as? String ?? "TCP"
        mtu = (object["mtu"] as? NSNumber)?.stringValue ?? "0"
        multiplexing = ((object["multiplexing"] as? [String: Any])?["level"] as? String ?? "MULTIPLEXING_LOW").replacingOccurrences(of: "MULTIPLEXING_", with: "")
        if multiplexing == "DEFAULT" { multiplexing = "LOW" }
        handshake = (object["handshakeMode"] as? String ?? "HANDSHAKE_STANDARD").replacingOccurrences(of: "HANDSHAKE_", with: "")
    }
    public func profile(replacing original: MieruProfile? = nil) throws -> MieruProfile {
        var object = try original?.object() ?? [:]
        object["profileName"] = name.trimmingCharacters(in: .whitespacesAndNewlines)
        var user = object["user"] as? [String: Any] ?? [:]
        if password.isEmpty, let oldName = user["name"] as? String, oldName != username, user["hashedPassword"] != nil {
            throw MieruDiagnostic(stage: "configuration", code: "password", message: "Re-enter the password when changing a hashed-profile username")
        }
        user["name"] = username
        if !password.isEmpty || user["hashedPassword"] == nil { user["password"] = password; user.removeValue(forKey: "hashedPassword") }
        object["user"] = user
        var servers = object["servers"] as? [[String: Any]] ?? [[:]]
        if servers.isEmpty { servers = [[:]] }
        var endpoint = servers[0]
        var address = server.trimmingCharacters(in: .whitespacesAndNewlines)
        if address.hasPrefix("["), address.hasSuffix("]") { address.removeFirst(); address.removeLast() }
        var v4 = in_addr(); var v6 = in6_addr()
        let isIP = address.withCString { inet_pton(AF_INET, $0, &v4) == 1 || inet_pton(AF_INET6, $0, &v6) == 1 }
        endpoint.removeValue(forKey: "ipAddress"); endpoint.removeValue(forKey: "domainName")
        endpoint[isIP ? "ipAddress" : "domainName"] = address
        var bindings = endpoint["portBindings"] as? [[String: Any]] ?? [[:]]
        if bindings.isEmpty { bindings = [[:]] }
        var binding: [String: Any] = ["protocol": transport]
        let ports = self.ports.trimmingCharacters(in: .whitespacesAndNewlines)
        if let port = Int(ports) { binding["port"] = port } else { binding["portRange"] = ports }
        bindings[0] = binding; endpoint["portBindings"] = bindings; servers[0] = endpoint; object["servers"] = servers
        guard let unit = Int(mtu.isEmpty ? "0" : mtu) else {
            throw MieruDiagnostic(stage: "configuration", code: "mtu", message: "MTU must be a whole number; 0 selects Automatic")
        }
        object["mtu"] = unit
        object["multiplexing"] = ["level": "MULTIPLEXING_" + multiplexing]
        object["handshakeMode"] = "HANDSHAKE_" + handshake
        let input = try JSONSerialization.data(withJSONObject: object)
        let normalized = try MieruConfiguration.normalize(input)
        guard var result = normalized.profiles.first else { throw MieruError.invalidConfiguration }
        if let original = original { result = MieruProfile(id: original.id, configuration: result.configuration) }
        return result
    }
}

public struct MieruProfileCollection: Codable, Equatable {
    public var profiles: [MieruProfile]
    public var activeID: UUID?
    public var active: MieruProfile? { profiles.first(where: { $0.id == activeID }) }
    public init(profiles: [MieruProfile] = [], activeID: UUID? = nil) { self.profiles = profiles; self.activeID = activeID }
}

public enum MieruConfiguration {
    public static func normalize(_ input: Data) throws -> MieruProfileCollection {
        guard !input.isEmpty, input.count <= 1024 * 1024 else {
            throw MieruDiagnostic(stage: "configuration", code: "size", message: "Configuration must be between 1 byte and 1 MiB")
        }
        var output = Data(count: 4 * 1024 * 1024)
        let result = input.withUnsafeBytes { bytes in output.withUnsafeMutableBytes { out in
            MieruConfigNormalize(bytes.baseAddress?.assumingMemoryBound(to: CChar.self), Int64(bytes.count), out.baseAddress, Int64(out.count))
        } }
        guard result > 0, result <= Int64(output.count) else { throw MieruError.invalidConfiguration }
        output.count = Int(result)
        guard let response = try JSONSerialization.jsonObject(with: output) as? [String: Any] else { throw MieruError.invalidConfiguration }
        if let error = response["error"] { throw try JSONDecoder().decode(MieruDiagnostic.self, from: JSONSerialization.data(withJSONObject: error)) }
        guard let config = response["configuration"] as? [String: Any], let raw = config["profiles"] as? [[String: Any]] else { throw MieruError.invalidConfiguration }
        let profiles = try raw.map { MieruProfile(configuration: try JSONSerialization.data(withJSONObject: $0)) }
        let selected = profiles.first(where: { $0.name == config["activeProfile"] as? String })
        guard let active = selected else { throw MieruError.invalidConfiguration }
        return MieruProfileCollection(profiles: profiles, activeID: active.id)
    }

    @discardableResult
    public static func probe(_ profile: MieruProfile, completion: @escaping (Result<MieruProbeResult, Error>) -> Void) -> MieruConnectionCheck {
        let check = MieruConnectionCheck()
        DispatchQueue.global(qos: .userInitiated).async {
            let response: Result<MieruProbeResult, Error>
            do {
                var output = Data(count: 8192)
                let result = profile.configuration.withUnsafeBytes { input in output.withUnsafeMutableBytes { out in
                    "149.154.167.50".withCString { target in
                        MieruProbeRun(check.handle, input.baseAddress?.assumingMemoryBound(to: CChar.self), Int64(input.count), target, 443, 10000, out.baseAddress, Int64(out.count))
                    }
                } }
                guard result > 0, result <= Int64(output.count) else { throw MieruError.network }
                output.count = Int(result)
                response = .success(try JSONDecoder().decode(MieruProbeResult.self, from: output))
            } catch { response = .failure(error) }
            DispatchQueue.main.async { if !check.cancelled { completion(response) } }
        }
        return check
    }
}

public final class MieruConnectionCheck {
    fileprivate let handle = MieruProbeNew()
    private let lock = NSLock()
    private var isCancelled = false
    fileprivate var cancelled: Bool { lock.lock(); defer { lock.unlock() }; return isCancelled }
    public func cancel() {
        lock.lock(); isCancelled = true; lock.unlock()
        MieruProbeCancel(handle)
    }
    deinit { MieruProbeCancel(handle) }
}

protocol MieruSecretStorage {
    func read() throws -> Data?
    func write(_ data: Data) throws
}

struct MieruKeychainStorage: MieruSecretStorage {
    private let query: [String: Any]
    init(service: String = "org.dimadr.Telegramita.Mieru.profiles.v2") {
        query = [kSecClass as String: kSecClassGenericPassword, kSecAttrService as String: service,
                 kSecAttrAccount as String: "vault", kSecAttrSynchronizable as String: false]
    }
    func read() throws -> Data? {
        var attributes = query; attributes[kSecReturnData as String] = true; attributes[kSecMatchLimit as String] = kSecMatchLimitOne
        var result: CFTypeRef?
        let status = SecItemCopyMatching(attributes as CFDictionary, &result)
        if status == errSecItemNotFound { return nil }
        guard status == errSecSuccess, let data = result as? Data else { throw failure("read", status) }
        return data
    }
    func write(_ data: Data) throws {
        let status = SecItemUpdate(query as CFDictionary, [kSecValueData as String: data] as CFDictionary)
        if status == errSecItemNotFound {
            var attributes = query; attributes[kSecValueData as String] = data
            attributes[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly
            let added = SecItemAdd(attributes as CFDictionary, nil)
            guard added == errSecSuccess else { throw failure("save", added) }
        } else if status != errSecSuccess { throw failure("save", status) }
    }
    private func failure(_ action: String, _ status: OSStatus) -> MieruDiagnostic {
        return MieruDiagnostic(stage: "keychain", code: "osstatus_\(status)", message: "Cannot \(action) Mieru profiles in Keychain (OSStatus \(status)).")
    }
}

final class MieruProfileRepository {
    private let storage: MieruSecretStorage
    init(storage: MieruSecretStorage = MieruKeychainStorage()) { self.storage = storage }
    func read() throws -> MieruProfileCollection {
        guard let data = try storage.read() else { return MieruProfileCollection() }
        do {
            let value = try JSONDecoder().decode(MieruProfileCollection.self, from: data)
            guard value.profiles.count <= 100, Set(value.profiles.map { $0.id }).count == value.profiles.count,
                  value.profiles.isEmpty || value.active != nil else { throw MieruError.invalidConfiguration }
            return value
        } catch { throw MieruDiagnostic(stage: "keychain", code: "profile_data", message: "Saved Mieru profiles could not be decoded; existing data was not replaced.") }
    }
    func write(_ collection: MieruProfileCollection) throws {
        guard collection.profiles.count <= 100, collection.profiles.isEmpty || collection.active != nil,
              Set(collection.profiles.map { $0.id }).count == collection.profiles.count,
              Set(collection.profiles.map { $0.name }).count == collection.profiles.count else {
            throw MieruDiagnostic(stage: "configuration", code: "profiles", message: "Profile names must be unique and an active profile must be selected.")
        }
        for profile in collection.profiles { _ = try MieruConfiguration.normalize(profile.configuration) }
        try storage.write(JSONEncoder().encode(collection))
    }
}
