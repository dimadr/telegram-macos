const fs = require('node:fs');
const { spawn, spawnSync } = require('node:child_process');

async function main() {
    const report = { status: 'NOT TESTED', scope: 'App process startup only; no Telegram account or Mieru server is configured' };
    const path = '.mieru-work/logs/startup-smoke.json';
    if (process.platform !== 'darwin' || spawnSync('pgrep', ['-x', 'WindowServer']).status !== 0) {
        report.reason = 'A macOS graphical session is unavailable';
        fs.writeFileSync(path, JSON.stringify(report, null, 2));
        console.log(JSON.stringify(report));
        return;
    }
    const executable = '.mieru-work/DerivedData/Build/Products/Debug/Telegram.app/Contents/MacOS/Telegram';
    const root = require('node:path').join(require('node:os').homedir(), 'Library/Application Support/org.dimadr.Telegramita/v1');
    if (fs.existsSync(root)) throw new Error('First-launch test requires an absent Telegramita data root; existing data will not be removed');
    report.clean_storage_before_launch = true;
    const output = fs.openSync('.mieru-work/logs/app-startup.log', 'w');
    let child;
    try {
        child = spawn(executable, [], { stdio: ['ignore', output, output] });
        await new Promise((resolve, reject) => {
            const timer = setTimeout(resolve, 30000);
            child.once('error', error => { clearTimeout(timer); reject(error); });
            child.once('exit', () => { clearTimeout(timer); resolve(); });
        });
        report.status = child.exitCode === null && child.signalCode === null ? 'PASS' : 'FAIL';
        report.isolated_database = fs.existsSync(root + '/accounts-metadata/db/db_sqlite');
        report.atomic_state_created = fs.existsSync(root + '/accounts-metadata/atomic-state');
        report.account_manager_ready = fs.readFileSync('.mieru-work/logs/app-startup.log', 'utf8').includes('Telegramita account manager ready');
        if (!report.isolated_database || !report.atomic_state_created || !report.account_manager_ready) report.status = 'FAIL';
        report.exit_code = child.exitCode;
        report.signal = child.signalCode;
    } catch (error) {
        report.status = 'FAIL';
        report.reason = error.message;
    } finally {
        if (child && child.exitCode === null && child.signalCode === null) {
            child.kill('SIGTERM');
            await new Promise(resolve => {
                const timer = setTimeout(() => { child.kill('SIGKILL'); resolve(); }, 5000);
                child.once('exit', () => { clearTimeout(timer); resolve(); });
            });
        }
        fs.closeSync(output);
        fs.writeFileSync(path, JSON.stringify(report, null, 2));
    }
    console.log(JSON.stringify(report));
    if (report.status === 'FAIL') process.exitCode = 1;
}

main().catch(error => { console.error(error.message); process.exitCode = 1; });
