import Foundation

/// Global socket path for signal handler cleanup (can't capture locals in @convention(c)).
private var globalSocketPath: UnsafeMutablePointer<CChar>?

private func signalHandler(_: Int32) {
    if let path = globalSocketPath {
        unlink(path)
        free(path)
    }
    exit(0)
}

/// Runs ax_server as a persistent Unix socket server.
/// Same NDJSON protocol as the stdin/stdout mode — one JSON request per line,
/// one JSON response per line. Accepts one client at a time; when the client
/// disconnects, accepts the next connection.
func runSocketServer(path socketPath: String) {
    // Clean up stale socket file
    unlink(socketPath)

    // Register signal handlers for clean shutdown
    globalSocketPath = strdup(socketPath)
    signal(SIGINT, signalHandler)
    signal(SIGTERM, signalHandler)

    // Create Unix domain socket
    let fd = socket(AF_UNIX, SOCK_STREAM, 0)
    guard fd >= 0 else {
        FileHandle.standardError.write("ax_server: failed to create socket\n".data(using: .utf8)!)
        exit(1)
    }

    var addr = sockaddr_un()
    addr.sun_family = sa_family_t(AF_UNIX)
    let pathBytes = socketPath.utf8CString
    guard pathBytes.count <= MemoryLayout.size(ofValue: addr.sun_path) else {
        FileHandle.standardError.write("ax_server: socket path too long\n".data(using: .utf8)!)
        exit(1)
    }
    withUnsafeMutablePointer(to: &addr.sun_path) { sunPath in
        sunPath.withMemoryRebound(to: CChar.self, capacity: pathBytes.count) { dst in
            for i in 0..<pathBytes.count {
                dst[i] = pathBytes[i]
            }
        }
    }

    let bindResult = withUnsafePointer(to: &addr) { ptr in
        ptr.withMemoryRebound(to: sockaddr.self, capacity: 1) { sockPtr in
            bind(fd, sockPtr, socklen_t(MemoryLayout<sockaddr_un>.size))
        }
    }
    guard bindResult == 0 else {
        FileHandle.standardError.write("ax_server: bind failed: \(String(cString: strerror(errno)))\n".data(using: .utf8)!)
        exit(1)
    }

    guard listen(fd, 1) == 0 else {
        FileHandle.standardError.write("ax_server: listen failed\n".data(using: .utf8)!)
        exit(1)
    }

    // Write "ready" to stdout so the launcher knows the socket is listening
    print("ready")
    fflush(stdout)

    let enc = JSONEncoder()
    enc.outputFormatting = [.sortedKeys]
    let dec = JSONDecoder()

    // Accept one client, serve it, then exit.
    // Each shan instance launches its own ax_server; there's no reason to
    // accept another connection after the client disconnects.
    let clientFD = accept(fd, nil, nil)
    guard clientFD >= 0 else {
        signalHandler(0)
        return
    }

    let input = FileHandle(fileDescriptor: clientFD, closeOnDealloc: false)
    let output = FileHandle(fileDescriptor: clientFD, closeOnDealloc: false)

    handleClient(input: input, output: output, encoder: enc, decoder: dec)

    close(clientFD)
    close(fd)
    // Client disconnected — clean up socket and exit
    signalHandler(0)
}

/// Process requests from a single client connection until it disconnects.
private func handleClient(
    input: FileHandle,
    output: FileHandle,
    encoder: JSONEncoder,
    decoder: JSONDecoder
) {
    // Read data in chunks and split on newlines
    var buffer = Data()

    while true {
        let chunk = input.availableData
        if chunk.isEmpty { break } // EOF — client disconnected

        buffer.append(chunk)

        // Process all complete lines in buffer
        while let newlineIndex = buffer.firstIndex(of: UInt8(ascii: "\n")) {
            let lineData = buffer[buffer.startIndex..<newlineIndex]
            buffer = buffer[buffer.index(after: newlineIndex)...]

            guard !lineData.isEmpty else { continue }

            guard let req = try? decoder.decode(Request.self, from: Data(lineData)) else {
                let resp = Response(id: 0, error: ErrorInfo(code: -1, message: "Invalid JSON request"))
                writeToHandle(resp, encoder: encoder, output: output)
                continue
            }

            let params = req.params ?? Params(
                pid: nil, maxDepth: nil, semanticBudget: nil, filter: nil,
                path: nil, expectedRole: nil, value: nil, appName: nil,
                query: nil, role: nil, identifier: nil, type: nil,
                x: nil, y: nil, button: nil, clicks: nil,
                key: nil, modifiers: nil, dx: nil, dy: nil,
                windowTitle: nil, verify: nil, condition: nil,
                timeout: nil, interval: nil, roles: nil, maxLabels: nil
            )

            let response = dispatch(id: req.id, method: req.method, params: params)
            writeToHandle(response, encoder: encoder, output: output)
        }
    }
}

/// Write a JSON response followed by a newline to the given file handle.
private func writeToHandle(_ resp: Response, encoder: JSONEncoder, output: FileHandle) {
    guard let data = try? encoder.encode(resp),
          var str = String(data: data, encoding: .utf8) else { return }
    str += "\n"
    if let bytes = str.data(using: .utf8) {
        output.write(bytes)
    }
}
