package appmon

import "testing"

const sampleDesktopEntry = `# A comment
[Desktop Entry]
Name=Visual Studio Code
Name[de]=Visual Studio Code
GenericName=Text Editor
Comment=Code Editing. Redefined.
Type=Application
Exec=/usr/share/code/code --unity-launch %F
Icon=vscode
Terminal=false
Categories=TextEditor;Development;

[Desktop Action new-window]
Name=New Window
Exec=/usr/share/code/code --new-window
`

func TestParseDesktopEntry(t *testing.T) {
	fields := parseDesktopEntry(sampleDesktopEntry)
	if fields["Name"] != "Visual Studio Code" {
		t.Errorf("Name=%q", fields["Name"])
	}
	if fields["Exec"] != "/usr/share/code/code --unity-launch %F" {
		t.Errorf("Exec=%q", fields["Exec"])
	}
	if fields["Icon"] != "vscode" {
		t.Errorf("Icon=%q", fields["Icon"])
	}
	// Keys from the [Desktop Action] group must not leak into the main entry.
	if fields["Exec"] == "/usr/share/code/code --new-window" {
		t.Error("desktop action Exec overwrote the main entry")
	}
	// Localised keys are ignored.
	if _, ok := fields["Name[de]"]; ok {
		t.Error("localised key kept")
	}
}

func TestParseDesktopEntryMissingGroup(t *testing.T) {
	if fields := parseDesktopEntry("Name=Nope\nExec=nope\n"); len(fields) != 0 {
		t.Fatalf("fields outside [Desktop Entry] parsed: %v", fields)
	}
}

func TestExecBase(t *testing.T) {
	cases := map[string]string{
		`/usr/share/code/code --unity-launch %F`: "code",
		`env FOO=bar /opt/app/bin/app --flag`:    "app",
		`sh -c "/opt/weird/app name"`:            "app name",
		`/usr/bin/foo`:                           "foo",
		`"quoted"`:                               "quoted",
		``:                                       "",
	}
	for in, want := range cases {
		if got := execBase(in); got != want {
			t.Errorf("execBase(%q)=%q want %q", in, got, want)
		}
	}
}

func TestIsTrue(t *testing.T) {
	for _, v := range []string{"true", "TRUE", "1", "yes"} {
		if !isTrue(v) {
			t.Errorf("isTrue(%q)=false", v)
		}
	}
	for _, v := range []string{"", "false", "0", "no"} {
		if isTrue(v) {
			t.Errorf("isTrue(%q)=true", v)
		}
	}
}
