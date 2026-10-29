const fs = require('node:fs');
const crypto = require('node:crypto');
const { pipeline } = require('node:stream/promises');
const { Readable } = require('node:stream');

const magic = Buffer.from('TGITA01\n');

function readExactly(fd, length, position) {
    const buffer = Buffer.alloc(length);
    let offset = 0;
    while (offset < length) {
        const count = fs.readSync(fd, buffer, offset, length - offset, position + offset);
        if (!count) throw new Error('Truncated encrypted artifact');
        offset += count;
    }
    return buffer;
}

async function main() {
    const [mode, keyPath, input, output] = process.argv.slice(2);
    if (!['seal', 'open'].includes(mode) || !keyPath || !input || !output) {
        throw new Error('Usage: node mieru-artifact.cjs seal|open key.pem input output');
    }
    if (fs.existsSync(output)) throw new Error('Output already exists');
    const temporary = output + '.partial-' + crypto.randomBytes(8).toString('hex');
    let key;
    try {
        if (mode === 'seal') {
            key = crypto.randomBytes(32);
            const nonce = crypto.randomBytes(12);
            const wrapped = crypto.publicEncrypt({ key: fs.readFileSync(keyPath), oaepHash: 'sha256' }, key);
            const size = Buffer.alloc(2);
            size.writeUInt16BE(wrapped.length);
            const header = Buffer.concat([magic, size, wrapped, nonce]);
            const cipher = crypto.createCipheriv('aes-256-gcm', key, nonce);
            cipher.setAAD(header);
            fs.writeFileSync(temporary, header, { flag: 'wx', mode: 0o600 });
            await pipeline(fs.createReadStream(input), cipher, fs.createWriteStream(temporary, { flags: 'a' }));
            fs.appendFileSync(temporary, cipher.getAuthTag());
        } else {
            const fd = fs.openSync(input, 'r');
            let header, tag, fileSize;
            try {
                fileSize = fs.fstatSync(fd).size;
                const prefix = readExactly(fd, 10, 0);
                if (!prefix.subarray(0, 8).equals(magic)) throw new Error('Invalid encrypted artifact');
                const wrappedLength = prefix.readUInt16BE(8);
                if (wrappedLength < 128 || wrappedLength > 8192) throw new Error('Invalid wrapped key size');
                header = readExactly(fd, 10 + wrappedLength + 12, 0);
                if (fileSize < header.length + 16) throw new Error('Truncated encrypted artifact');
                key = crypto.privateDecrypt({ key: fs.readFileSync(keyPath), oaepHash: 'sha256' }, header.subarray(10, 10 + wrappedLength));
                tag = readExactly(fd, 16, fileSize - 16);
            } finally { fs.closeSync(fd); }
            const decipher = crypto.createDecipheriv('aes-256-gcm', key, header.subarray(-12));
            decipher.setAAD(header);
            decipher.setAuthTag(tag);
            const content = fileSize === header.length + 16 ? Readable.from([]) : fs.createReadStream(input, { start: header.length, end: fileSize - 17 });
            await pipeline(content, decipher, fs.createWriteStream(temporary, { flags: 'wx', mode: 0o600 }));
        }
        if (fs.existsSync(output)) throw new Error('Output already exists');
        fs.renameSync(temporary, output);
        console.log('Artifact ' + mode + ' completed');
    } finally {
        if (key) key.fill(0);
        fs.rmSync(temporary, { force: true });
    }
}

main().catch(error => { console.error(error.message); process.exitCode = 1; });
