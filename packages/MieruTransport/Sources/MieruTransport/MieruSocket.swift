import Foundation
import MieruABI

public final class MieruSocket {
    private let handle: UInt64
    private let queue: DispatchQueue
    private let reads = DispatchQueue(label: "Telegramita.Mieru.read", qos: .userInitiated)
    private let writes = DispatchQueue(label: "Telegramita.Mieru.write", qos: .userInitiated)
    private let lock = NSLock()
    private var closed = false

    init(handle: UInt64, queue: DispatchQueue) {
        self.handle = handle
        self.queue = queue
    }

    public func connect(host: String, port: UInt16, timeout: TimeInterval, completion: @escaping (Result<Void, MieruError>) -> Void) {
        self.reads.async {
            let result = host.withCString { MieruConnectionDial(self.handle, $0, port, Self.milliseconds(timeout < 0 ? 86400 : timeout)) }
            if result < 0 { MieruRuntime.shared.reportConnectionFailure(self.handle) }
            self.deliver(result < 0 ? .failure(.from(Int64(result))) : .success(()), completion)
        }
    }

    public func read(maximum: Int, timeout: TimeInterval, completion: @escaping (Result<Data, MieruError>) -> Void) {
        self.reads.async {
            var data = Data(count: max(1, min(maximum, 256 * 1024)))
            let result = data.withUnsafeMutableBytes { MieruConnectionRead(self.handle, $0.baseAddress, Int64($0.count), Self.milliseconds(timeout)) }
            if result > 0 {
                data.count = Int(result)
                MieruRuntime.shared.report("Mieru is carrying traffic")
                self.deliver(.success(data), completion)
            } else {
                self.deliver(.failure(result == 0 ? .closed : .from(result)), completion)
            }
        }
    }

    public func write(_ data: Data, completion: @escaping (Result<Void, MieruError>) -> Void) {
        self.writes.async {
            var offset = 0
            while offset < data.count {
                let result = data.withUnsafeBytes { bytes in
                    MieruConnectionWrite(self.handle, bytes.baseAddress?.advanced(by: offset), Int64(min(bytes.count - offset, 256 * 1024)), 30000)
                }
                guard result > 0 else {
                    self.deliver(.failure(result == 0 ? .closed : .from(result)), completion)
                    return
                }
                offset += Int(result)
            }
            self.deliver(.success(()), completion)
        }
    }

    public func close() {
        self.lock.lock()
        let close = !self.closed
        self.closed = true
        self.lock.unlock()
        if close {
            let handle = self.handle
            DispatchQueue.global(qos: .userInitiated).async { MieruConnectionClose(handle) }
        }
    }

    private func deliver<T>(_ result: Result<T, MieruError>, _ completion: @escaping (Result<T, MieruError>) -> Void) {
        self.queue.async {
            self.lock.lock()
            let closed = self.closed
            self.lock.unlock()
            if !closed { completion(result) }
        }
    }

    private static func milliseconds(_ timeout: TimeInterval) -> Int64 {
        if timeout < 0 { return 0 }
        if !timeout.isFinite { return 30000 }
        return Int64(max(1, min(timeout * 1000, 86400000)))
    }

    deinit { self.close() }
}
