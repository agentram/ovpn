import Foundation
import Security

enum AccessStorage {
    enum Failure: Error { case keychain }

    static func read(_ name: String) throws -> Data? {
        var query = try query(name)
        query[kSecReturnData as String] = true
        query[kSecMatchLimit as String] = kSecMatchLimitOne
        var value: CFTypeRef?
        let status = SecItemCopyMatching(query as CFDictionary, &value)
        if status == errSecItemNotFound { return nil }
        guard status == errSecSuccess, let data = value as? Data else { throw Failure.keychain }
        return data
    }

    static func write(_ name: String, _ data: Data?) throws {
        let key = try query(name)
        guard let data else {
            let status = SecItemDelete(key as CFDictionary)
            guard status == errSecSuccess || status == errSecItemNotFound else { throw Failure.keychain }
            return
        }
        let value = [kSecValueData as String: data]
        let status = SecItemUpdate(key as CFDictionary, value as CFDictionary)
        if status == errSecItemNotFound {
            var item = key.merging(value) { _, new in new }
            item[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly
            guard SecItemAdd(item as CFDictionary, nil) == errSecSuccess else { throw Failure.keychain }
        } else if status != errSecSuccess {
            throw Failure.keychain
        }
    }

    private static func query(_ name: String) throws -> [String: Any] {
        var query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: "O Tunnel",
            kSecAttrAccount as String: name
        ]
        if name == "active-profile" {
            guard let group = Bundle.main.object(forInfoDictionaryKey: "OVPNKeychainGroup") as? String,
                  !group.isEmpty else { throw Failure.keychain }
            query[kSecAttrAccessGroup as String] = group
        }
        return query
    }
}
