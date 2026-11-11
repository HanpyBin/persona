import Foundation
import SwiftUI

// The text shown in file lists and spoken by VoiceOver; the original name remains the operation's identity.
nonisolated enum FileNameDisplay {
    struct Parts {
        let leading: String
        let body: String
        let trailing: String
    }

    private static func invisible(_ scalar: Unicode.Scalar) -> Bool {
        CharacterSet.whitespacesAndNewlines.contains(scalar) || scalar.value == 0x200B || scalar.value == 0xFEFF
    }

    static func needsMarker(_ name: String) -> Bool {
        guard let first = name.unicodeScalars.first, let last = name.unicodeScalars.last else { return false }
        return invisible(first) || invisible(last)
    }

    static func parts(_ name: String) -> Parts? {
        guard needsMarker(name) else { return nil }
        let scalars = Array(name.unicodeScalars)
        var start = 0, end = scalars.count
        while start < end && invisible(scalars[start]) { start += 1 }
        while end > start && invisible(scalars[end - 1]) { end -= 1 }
        let body = String(String.UnicodeScalarView(scalars[start..<end]))
        return Parts(leading: markers(scalars[0..<start]), body: body, trailing: markers(scalars[end..<scalars.count]))
    }

    static func label(_ name: String) -> String {
        guard let p = parts(name) else { return name }
        return p.leading + p.body + p.trailing
    }

    static func spoken(_ name: String) -> String {
        guard let p = parts(name) else { return name }
        if p.body.isEmpty { return String(localized: "Whitespace-only name \(p.leading)") }
        if !p.leading.isEmpty && !p.trailing.isEmpty {
            return String(localized: "\(p.body), leading whitespace \(p.leading), trailing whitespace \(p.trailing)")
        }
        if !p.leading.isEmpty { return String(localized: "\(p.body), leading whitespace \(p.leading)") }
        return String(localized: "\(p.body), trailing whitespace \(p.trailing)")
    }

    private static func markers(_ scalars: ArraySlice<Unicode.Scalar>) -> String {
        guard !scalars.isEmpty else { return "" }
        let chars = Array(scalars)
        var runs: [String] = [], index = 0
        while index < chars.count {
            var end = index + 1
            while end < chars.count && chars[end] == chars[index] { end += 1 }
            let count = end - index
            runs.append(whitespaceName(chars[index]) + (count > 1 ? " ×\(count)" : ""))
            index = end
        }
        return "[" + runs.joined(separator: ", ") + "]"
    }

    private static func whitespaceName(_ scalar: Unicode.Scalar) -> String {
        switch scalar.value {
        case 0x20: String(localized: "space [File Name Whitespace]")
        case 0xA0: String(localized: "non-breaking space [File Name Whitespace]")
        case 0x09: String(localized: "tab [File Name Whitespace]")
        case 0x0A: String(localized: "line break [File Name Whitespace]")
        case 0x0D: String(localized: "carriage return [File Name Whitespace]")
        default: String(format: "U+%04X", scalar.value)
        }
    }
}

struct FileNameView: View {
    let name: String

    var body: some View {
        if let p = FileNameDisplay.parts(name) {
            (Text(verbatim: p.leading).foregroundColor(.secondary) + Text(verbatim: p.body) + Text(verbatim: p.trailing).foregroundColor(.secondary))
                .accessibilityLabel(Text(verbatim: FileNameDisplay.spoken(name)))
        } else {
            Text(verbatim: name)
        }
    }
}
