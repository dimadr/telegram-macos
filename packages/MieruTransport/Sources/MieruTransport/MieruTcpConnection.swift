import Foundation
import MtProtoKit

public final class MieruTcpConnection: NSObject, MTTcpConnectionInterface {
    private struct Read {
        let length: Int
        let tag: Int
        let deadline: TimeInterval?
        var data = Data()
    }

    private weak var delegate: MTTcpConnectionInterfaceDelegate?
    private let queue: DispatchQueue
    private var socket: MieruSocket?
    private var reads: [Read] = []
    private var reading = false
    private var disconnected = false
    private var usageInfo: MTNetworkUsageCalculationInfo?
    private var usageManager: MTNetworkUsageManager?

    public init(delegate: MTTcpConnectionInterfaceDelegate, queue: DispatchQueue) {
        self.delegate = delegate
        self.queue = DispatchQueue(label: "Telegramita.Mieru.mtproto", target: queue)
        super.init()
    }

    public func setGetLogPrefix(_ getLogPrefix: (() -> String)?) {}
    public func setUsageCalculationInfo(_ usageCalculationInfo: MTNetworkUsageCalculationInfo?) {
        self.queue.async {
            if self.usageInfo !== usageCalculationInfo {
                self.usageInfo = usageCalculationInfo
                self.usageManager = usageCalculationInfo.map { MTNetworkUsageManager(info: $0) }
            }
        }
    }

    public func connect(toHost host: String, onPort port: UInt16, viaInterface: String?, withTimeout timeout: TimeInterval, error: NSErrorPointer) -> Bool {
        self.queue.async {
            guard viaInterface == nil || viaInterface?.isEmpty == true else { self.fail(.unsupported); return }
            self.connect(host: host, port: port, timeout: timeout)
        }
        return true
    }

    private func connect(host: String, port: UInt16, timeout: TimeInterval) {
        guard !self.disconnected, self.socket == nil else { self.fail(.closed); return }
        do {
            let socket = try MieruRuntime.shared.makeSocket(queue: self.queue)
            self.socket = socket
            socket.connect(host: host, port: port, timeout: timeout) { [weak self] result in
                guard let self = self, !self.disconnected else { return }
                switch result {
                case .success: self.delegate?.connectionInterfaceDidConnect()
                case let .failure(error): self.fail(error)
                }
            }
        } catch let failure {
            self.fail(failure as? MieruError ?? .network)
        }
    }

    public func write(_ data: Data) {
        self.queue.async {
            self.socket?.write(data) { [weak self] result in
                switch result {
                case .success: self?.usageManager?.addOutgoingBytes(UInt(data.count), interface: MTNetworkUsageManagerInterfaceOther)
                case let .failure(error): self?.fail(error)
                }
            }
        }
    }

    public func readData(toLength length: UInt, withTimeout timeout: TimeInterval, tag: Int) {
        self.queue.async { self.enqueueRead(length: length, timeout: timeout, tag: tag) }
    }

    private func enqueueRead(length: UInt, timeout: TimeInterval, tag: Int) {
        guard !self.disconnected, length <= 16 * 1024 * 1024 else {
            self.fail(.invalidConfiguration)
            return
        }
        self.reads.append(Read(length: Int(length), tag: tag, deadline: timeout < 0 ? nil : ProcessInfo.processInfo.systemUptime + timeout))
        self.readNext()
    }

    private func readNext() {
        guard !self.reading, !self.disconnected, let request = self.reads.first, let socket = self.socket else { return }
        if request.data.count == request.length {
            self.reads.removeFirst()
            self.delegate?.connectionInterfaceDidRead(request.data, withTag: request.tag, networkType: 0)
            self.readNext()
            return
        }
        let timeout = request.deadline.map { $0 - ProcessInfo.processInfo.systemUptime } ?? -1
        if request.deadline != nil && timeout <= 0 { self.fail(.timeout); return }
        self.reading = true
        socket.read(maximum: request.length - request.data.count, timeout: timeout) { [weak self] result in
            guard let self = self, !self.disconnected, !self.reads.isEmpty else { return }
            self.reading = false
            switch result {
            case let .success(data):
                self.usageManager?.addIncomingBytes(UInt(data.count), interface: MTNetworkUsageManagerInterfaceOther)
                self.reads[0].data.append(data)
                if self.reads[0].data.count < self.reads[0].length {
                    self.delegate?.connectionInterfaceDidReadPartialData(ofLength: UInt(data.count), tag: self.reads[0].tag)
                }
                self.readNext()
            case let .failure(error): self.fail(error)
            }
        }
    }

    public func disconnect() { self.queue.async { self.fail(.closed) } }
    public func resetDelegate() { self.queue.async { self.delegate = nil } }

    private func fail(_ error: MieruError) {
        guard !self.disconnected else { return }
        self.disconnected = true
        self.socket?.close()
        self.socket = nil
        self.reads.removeAll()
        MieruRuntime.shared.report(error.message)
        self.delegate?.connectionInterfaceDidDisconnectWithError(NSError(domain: "Telegramita.Mieru", code: error.rawValue, userInfo: [NSLocalizedDescriptionKey: error.message]))
    }

    deinit { self.socket?.close() }
}
