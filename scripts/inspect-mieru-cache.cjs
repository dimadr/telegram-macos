const fs = require('node:fs');
const path = require('node:path');

const phase = process.argv[2];
if (!['restored', 'final'].includes(phase)) throw new Error('Expected restored or final');
const root = process.cwd();
const derived = path.join(root, '.mieru-work/DerivedData');
const groups = {};
const records = [];
const suffixes = ['.swiftinterface', '.swiftmodule', '.swiftdoc', '.swiftsourceinfo', '.swiftdeps', '.abi.json', '.o', '.a', '.h', '.modulemap', '.d', '.pcm'];

function walk(directory, group) {
    if (!fs.existsSync(directory)) return;
    for (const entry of fs.readdirSync(directory, { withFileTypes: true })) {
        const file = path.join(directory, entry.name);
        if (entry.isDirectory()) {
            if (!/\.(app|appex|xpc|bundle)$/.test(entry.name)) walk(file, group);
        } else if (entry.isFile()) {
            const stat = fs.statSync(file);
            const suffix = suffixes.find(value => file.endsWith(value)) || 'other';
            const key = group + ':' + suffix;
            const value = groups[key] ||= { files: 0, bytes: 0, allocatedBytes: 0 };
            value.files++;
            value.bytes += stat.size;
            value.allocatedBytes += Number.isFinite(stat.blocks) ? stat.blocks * 512 : stat.size;
            if (entry.name.startsWith('build.db') || entry.name.endsWith('.swiftdeps') && records.length < 12) {
                records.push({ path: path.relative(root, file), bytes: stat.size, modified: stat.mtime.toISOString() });
            }
        }
    }
}

for (const group of ['Build/Intermediates.noindex', 'Build/Products', 'ModuleCache.noindex', 'CompilationCache.noindex']) {
    walk(path.join(derived, group), group);
}
const report = { phase, workspace: root, xcode: process.env.MIERU_APP_XCODE_VERSION, arch: process.arch, groups, records };
const logs = path.join(root, '.mieru-work/logs');
fs.mkdirSync(logs, { recursive: true });
fs.writeFileSync(path.join(logs, 'swift-cache-' + phase + '.json'), JSON.stringify(report, null, 2) + '\n');
console.log(JSON.stringify({ phase, groups: Object.keys(groups).length, files: Object.values(groups).reduce((sum, value) => sum + value.files, 0) }));
