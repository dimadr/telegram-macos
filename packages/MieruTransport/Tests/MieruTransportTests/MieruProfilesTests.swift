import Foundation
import Security
import XCTest
import MieruABI
@testable import MieruTransport

private final class MemorySecrets: MieruSecretStorage {
    var data: Data?
    var writes = 0
    var denyWrite = false
    func read() throws -> Data? { data }
    func write(_ data: Data) throws {
        if denyWrite { throw MieruDiagnostic(stage: "keychain", code: "denied", message: "Test storage denied") }
        self.data = data; writes += 1
    }
}

final class MieruProfilesTests: XCTestCase {
    private func fields() -> MieruProfileFields {
        var value = MieruProfileFields()
        value.name = "Unit profile"; value.server = "2001:db8::1"; value.ports = "4012-4021"
        value.username = "unit-user"; value.password = "TEST_SECRET_DO_NOT_LOG"; value.transport = "UDP"
        return value
    }

    func testManualProfileRoundTripAndDefaults() throws {
        let input = fields()
        XCTAssertEqual(input.mtu, "0")
        let profile = try input.profile()
        let output = try MieruProfileFields(profile: profile)
        XCTAssertTrue(input == output)
        var edited = output; edited.name = "Renamed"
        let replacement = try edited.profile(replacing: profile)
        XCTAssertEqual(replacement.id, profile.id)
        XCTAssertEqual(replacement.name, "Renamed")
    }

    func testImportedHashedPasswordAndAdditionalEndpointsArePreserved() throws {
        let raw: [String: Any] = ["profileName": "Hashed", "user": ["name": "unit-user", "hashedPassword": String(repeating: "ab", count: 32)],
            "servers": [["domainName": "mita.example.org", "portBindings": [["port": 443, "protocol": "TCP"], ["port": 444, "protocol": "UDP"]]],
                        ["ipAddress": "127.0.0.1", "portBindings": [["port": 445, "protocol": "TCP"]]]]]
        let profile = try XCTUnwrap(MieruConfiguration.normalize(JSONSerialization.data(withJSONObject: raw)).active)
        var edited = try MieruProfileFields(profile: profile)
        edited.name = "Renamed"
        let saved = try edited.profile(replacing: profile)
        let object = try saved.object()
        XCTAssertEqual((object["servers"] as? [[String: Any]])?.count, 2)
        XCTAssertEqual(((object["servers"] as? [[String: Any]])?.first?["portBindings"] as? [[String: Any]])?.count, 2)
        XCTAssertTrue((object["user"] as? [String: Any])?["hashedPassword"] as? String == String(repeating: "ab", count: 32))
        edited.username = "different-user"
        XCTAssertThrowsError(try edited.profile(replacing: profile))
    }

    func testRepositoryCommitsWholeCollectionOrPreservesOldData() throws {
        let secrets = MemorySecrets()
        let repository = MieruProfileRepository(storage: secrets)
        XCTAssertTrue(try repository.read().profiles.isEmpty)
        let profile = try fields().profile()
        let collection = MieruProfileCollection(profiles: [profile], activeID: profile.id)
        try repository.write(collection)
        XCTAssertTrue(try repository.read() == collection)
        let previous = secrets.data
        var invalid = collection
        invalid.profiles.append(profile)
        XCTAssertThrowsError(try repository.write(invalid))
        XCTAssertTrue(previous == secrets.data)
        XCTAssertEqual(secrets.writes, 1)
        secrets.denyWrite = true
        XCTAssertThrowsError(try repository.write(collection)) { error in
            XCTAssertEqual((error as? MieruDiagnostic)?.stage, "keychain")
        }
        XCTAssertTrue(previous == secrets.data)
        secrets.data = Data("invalid".utf8)
        XCTAssertThrowsError(try repository.read())
        XCTAssertEqual(secrets.writes, 1)
    }

    func testKeychainRoundTripUsesOnlyAnIsolatedTestItem() throws {
        let service = "org.dimadr.Telegramita.Mieru.tests.\(UUID().uuidString)"
        let storage = MieruKeychainStorage(service: service)
        defer {
            SecItemDelete([kSecClass as String: kSecClassGenericPassword, kSecAttrService as String: service,
                           kSecAttrAccount as String: "vault", kSecAttrSynchronizable as String: false] as CFDictionary)
        }
        XCTAssertNil(try storage.read())
        let repository = MieruProfileRepository(storage: storage)
        let profile = try fields().profile()
        let collection = MieruProfileCollection(profiles: [profile], activeID: profile.id)
        try repository.write(collection)
        XCTAssertTrue(try repository.read() == collection)
        var updated = collection
        var changed = fields(); changed.password = "OTHER_TEST_SECRET"
        updated.profiles = [try changed.profile(replacing: profile)]
        try repository.write(updated)
        XCTAssertTrue(try repository.read() == updated)
    }

    func testValidationErrorsNeverContainInputSecrets() throws {
        var invalid = fields(); invalid.ports = "65536"
        XCTAssertThrowsError(try invalid.profile()) { error in
            XCTAssertFalse(error.localizedDescription.contains(invalid.password))
            XCTAssertTrue((error as? MieruDiagnostic)?.message.contains("65535") == true)
        }
        invalid = fields(); invalid.server = "https://example.org"
        XCTAssertThrowsError(try invalid.profile())
        invalid = fields(); invalid.password = ""
        XCTAssertThrowsError(try invalid.profile())
    }

    func testClientLifecycleAndFailClosedHandles() throws {
        let profile = try fields().profile()
        for _ in 0..<2 {
            let client = MieruClientNew()
            XCTAssertNotEqual(client, 0)
            defer { MieruClientFree(client) }
            XCTAssertEqual(MieruConnectionNew(client), 0)
            XCTAssertLessThan(MieruClientStart(client), 0)
            let stored = profile.configuration.withUnsafeBytes { MieruClientStore(client, $0.baseAddress?.assumingMemoryBound(to: CChar.self), Int64($0.count)) }
            XCTAssertEqual(stored, 0)
            XCTAssertEqual(MieruClientStart(client), 0)
            let connection = MieruConnectionNew(client)
            XCTAssertNotEqual(connection, 0)
            MieruClientStop(client)
            XCTAssertEqual(MieruConnectionNew(client), 0)
            XCTAssertLessThan(MieruClientStart(client), 0)
            MieruConnectionClose(connection)
        }
    }

    func testInitializedClientIsNeverReportedAsConnected() throws {
        let value = try JSONDecoder().decode(MieruProbeResult.self, from: Data("{\"configValid\":true,\"clientStarted\":true,\"connectionEstablished\":false}".utf8))
        XCTAssertTrue(value.summary.contains("CONFIG VALID"))
        XCTAssertTrue(value.summary.contains("CLIENT STARTED"))
        XCTAssertFalse(value.summary.contains("CONNECTION ESTABLISHED"))
    }
}
