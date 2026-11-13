package appmon

// The Info.plist, .icns and PNG fixtures are embedded as text: this repo
// rejects committed binary files, and they are the only way to exercise the
// bplist00 parser and the icns chunk slicer without a machine full of apps.

const fixtureXMLPlist = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleName</key>
	<string>Fixture</string>
	<key>CFBundleDisplayName</key>
	<string>Fixture App</string>
	<key>CFBundleIdentifier</key>
	<string>com.example.fixture</string>
	<key>CFBundleShortVersionString</key>
	<string>2.5.1</string>
	<key>CFBundleVersion</key>
	<string>251</string>
	<key>LSMinimumSystemVersion</key>
	<string>13.0</string>
	<key>NSHighResolutionCapable</key>
	<true/>
	<key>Nested</key>
	<dict>
		<key>CFBundleName</key>
		<string>Should Not Win</string>
		<key>Items</key>
		<array>
			<string>a</string>
			<string>b</string>
		</array>
	</dict>
</dict>
</plist>
`

const fixtureBinaryPlistBase64 = "YnBsaXN0MDDYAQIDBAUGBwgJCgsREhMUFV8QGkNGQnVuZGxlU2hvcnRWZXJzaW9uU3Ry" +
	"aW5nXxASQ0ZCdW5kbGVJZGVudGlmaWVyVk5lc3RlZFxDRkJ1bmRsZU5hbWVfEBdOU0hp" +
	"Z2hSZXNvbHV0aW9uQ2FwYWJsZV8QE0NGQnVuZGxlRGlzcGxheU5hbWVfEBZMU01pbmlt" +
	"dW1TeXN0ZW1WZXJzaW9uXxAPQ0ZCdW5kbGVWZXJzaW9uVTIuNS4xXxATY29tLmV4YW1w" +
	"bGUuZml4dHVyZdIMBA0QVUl0ZW1zog4PUWFRYl5TaG91bGQgTm90IFdpbldGaXh0dXJl" +
	"CVtGaXh0dXJlIEFwcFQxMy4wUzI1MQAIABkANgBLAFIAXwB5AI8AqAC6AMAA1gDbAOEA" +
	"5ADmAOgA9wD/AQABDAERAAAAAAAAAgEAAAAAAAAAFgAAAAAAAAAAAAAAAAAAARU="

const fixtureICNSBase64 = "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" +
	"mqkAAAAASUVORK5CYII="

const fixtureIconPNGBase64 = "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"
