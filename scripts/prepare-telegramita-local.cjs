const fs = require('node:fs');
const { execFileSync } = require('node:child_process');

function edit(file, transform) {
    const before = fs.readFileSync(file, 'utf8');
    const after = transform(before);
    if (after === before) throw new Error('Expected identity change: ' + file);
    fs.writeFileSync(file, after);
}
function once(text, from, to) {
    if (!text.includes(from) || text.indexOf(from) !== text.lastIndexOf(from)) throw new Error('Source context mismatch: ' + from.slice(0, 70));
    return text.replace(from, to);
}

edit('packages/ApiCredentials/Sources/ApiCredentials/Config.swift', text => {
    text = once(text, 'return "ru.keepcoder.Telegram"', 'return "org.dimadr.Telegramita"');
    text = once(text, 'return "6N38VWS5BX"', 'return ""');
    const start = text.indexOf('    public static var containerURL:');
    const end = text.indexOf('    public static var appData:');
    if (start < 0 || end < start) throw new Error('Missing ApiEnvironment storage declarations');
    text = text.slice(0, start) + `    public static var legacyContainerURL: URL? {
        FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask).first?.appendingPathComponent(bundleId, isDirectory: true)
    }

    public static var containerURL: URL? {
        guard let root = legacyContainerURL else { return nil }
        let url = root.appendingPathComponent("v1", isDirectory: true)
        do {
            try FileManager.default.createDirectory(at: root, withIntermediateDirectories: true, attributes: [.posixPermissions: 0o700])
            try FileManager.default.createDirectory(at: url, withIntermediateDirectories: true, attributes: [.posixPermissions: 0o700])
            let probe = url.appendingPathComponent(".write-probe-" + UUID().uuidString)
            try Data([0]).write(to: probe, options: .withoutOverwriting)
            try FileManager.default.removeItem(at: probe)
            NSLog("Telegramita storage: %@", url.path.replacingOccurrences(of: NSHomeDirectory(), with: "$HOME"))
            return url
        } catch {
            let failure = error as NSError
            NSLog("Telegramita storage creation failed: domain=%@ code=%ld", failure.domain, failure.code)
            return nil
        }
    }

    public static func migrate() {}

    public static var group: String { bundleId }

` + text.slice(end);
    const prefix = text.indexOf('    public static var prefix:');
    const version = text.indexOf('    public static var version:');
    if (prefix < 0 || version < prefix) throw new Error('Missing channel prefix');
    return text.slice(0, prefix) + '    public static var prefix: String { "v1" }\n\n' + text.slice(version);
});
edit('Telegram-Mac/app/AppDelegate.swift', text => {
    text += '\nprivate let telegramitaAutomaticUpdatesEnabled = false\n';
    text = text.replace(/^(\s*)(updater_resetWithUpdaterSource\([^\n]+\))\s*$/gm, '$1if telegramitaAutomaticUpdatesEnabled { $2 }');
    text = once(text, 'keychainScope: ApiEnvironment.prefix == "debug" ? "debug" : nil', 'keychainScope: "telegramita-v1"');
    if (!text.includes('removeDatabaseOnError: true, accessChallenge:')) throw new Error('Missing account manager recovery policy');
    text = text.replaceAll('removeDatabaseOnError: true, accessChallenge:', 'removeDatabaseOnError: false, accessChallenge:');
    const marker = '        if let crash = self.rustEngineCrash {';
    return once(text, marker, `        let _ = accountManager.transaction { _ -> Void in
            FileHandle.standardError.write(Data("Telegramita account manager ready\\n".utf8))
        }.start()

` + marker);
});
edit('packages/MieruTransport/Sources/MieruTransport/MieruRuntime.swift', text => once(text, 'UserDefaults(suiteName: "6N38VWS5BX.ru.keepcoder.Telegram")!', 'UserDefaults(suiteName: "org.dimadr.Telegramita.mieru")!'));
edit('submodules/telegram-ios/submodules/PasscodeCore/Sources/PasscodeKeychainScope.swift', text => {
    const marker = '        return Self.migrateLegacyItems(passcodeService: passcodeService, walletServicePrefixes: walletServicePrefixes, environment: environment, keychain: Self.system)';
    return once(text, marker, '        if environment.keychainScope == "telegramita-v1" { return .nothingToMigrate }\n' + marker);
});
edit('Telegram.xcodeproj/project.pbxproj', text => text.replaceAll('ru.keepcoder.Telegram', 'org.dimadr.Telegramita'));
edit('submodules/telegram-ios/submodules/Postbox/Sources/SqliteValueBox.swift', text => {
    text = once(text, 'import Foundation', 'import Foundation\nimport Darwin');
    for (const pragma of ['journal_mode=WAL', 'user_version=123']) {
        const context = `testDatabase.execute("PRAGMA ${pragma}")\n                if !resultCode {\n                    throw TempError.generic`;
        const replacement = `testDatabase.execute("PRAGMA ${pragma}")\n                if !resultCode {\n                    FileHandle.standardError.write(Data("Telegramita SQLite probe ${pragma}: rc=\\(sqlite3_errcode(testDatabase.handle)) extended=\\(sqlite3_extended_errcode(testDatabase.handle)) errno=\\(errno)\\n".utf8))\n                    throw TempError.generic`;
        text = once(text, context, replacement);
    }
    const marker = '            } catch {\n                let _ = try? FileManager.default.removeItem(atPath: tempPath)';
    return once(text, marker, `            } catch {
                let failure = error as NSError
                let diagnostic = "Telegramita SQLite write probe failed: open_rc=\\(openError.code) open_errno=\\(openError.systemErrno) probe_errno=\\(errno) domain=\\(failure.domain) code=\\(failure.code)\\n"
                FileHandle.standardError.write(Data(diagnostic.utf8))
                let _ = try? FileManager.default.removeItem(atPath: tempPath)`);
});
for (const [file, identifier] of [['Telegram-Mac/Info.plist', 'org.dimadr.Telegramita'], ['TelegramShare/Info.plist', 'org.dimadr.Telegramita.Share']]) {
    const info = JSON.parse(execFileSync('/usr/bin/plutil', ['-convert', 'json', '-o', '-', file], { encoding: 'utf8' }));
    info.CFBundleIdentifier = identifier;
    info.CFBundleName = file.startsWith('Telegram-Mac/') ? 'Telegramita' : 'TelegramitaShare';
    delete info.CFBundleURLTypes;
    delete info.CFBundleDocumentTypes;
    delete info.NSUserActivityTypes;
    info.SUEnableAutomaticChecks = false;
    info.SUAutomaticallyUpdate = false;
    delete info.SUFeedURL;
    execFileSync('/usr/bin/plutil', ['-convert', 'xml1', '-o', file, '-'], { input: JSON.stringify(info) });
}
console.log('Telegramita local identity prepared; no legacy migration or certificate-dependent App Group');
