import Foundation

/// Tiny async wrapper around `Process` so we never block the cooperative
/// thread pool waiting on a child process. Used for `tar` and probing the
/// installed CLI's `--version`.
public enum Subprocess {
    public struct Result: Sendable {
        public var exitCode: Int32
        public var stdout: String
        public var stderr: String
    }

    public static func run(_ executable: String, _ arguments: [String]) async throws -> Result {
        try await withCheckedThrowingContinuation { (cont: CheckedContinuation<Result, Error>) in
            let proc = Process()
            proc.executableURL = URL(fileURLWithPath: executable)
            proc.arguments = arguments
            let outPipe = Pipe(), errPipe = Pipe()
            proc.standardOutput = outPipe
            proc.standardError = errPipe
            proc.terminationHandler = { p in
                let out = String(data: outPipe.fileHandleForReading.readDataToEndOfFile(), encoding: .utf8) ?? ""
                let err = String(data: errPipe.fileHandleForReading.readDataToEndOfFile(), encoding: .utf8) ?? ""
                cont.resume(returning: Result(exitCode: p.terminationStatus, stdout: out, stderr: err))
            }
            do {
                try proc.run()
            } catch {
                cont.resume(throwing: error)
            }
        }
    }
}
