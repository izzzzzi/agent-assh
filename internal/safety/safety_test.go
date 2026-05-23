package safety

import "testing"

func TestCheckCommandBlocksDangerousCommands(t *testing.T) {
	tests := []struct {
		name    string
		command string
		rule    string
	}{
		{name: "rm recursive", command: "rm -rf /tmp/build", rule: "rm_recursive"},
		{name: "path rm recursive", command: "/bin/rm -rf /tmp/build", rule: "rm_recursive"},
		{name: "rm quoted recursive option", command: `rm "-rf" /tmp/build`, rule: "rm_recursive"},
		{name: "sudo rm recursive", command: "sudo rm -rf /var/www", rule: "rm_recursive"},
		{name: "sudo option rm recursive", command: "sudo -n rm -rf /var/www", rule: "rm_recursive"},
		{name: "sudo user rm recursive", command: "sudo -u root rm -rf /tmp/build", rule: "rm_recursive"},
		{name: "sudo long user rm recursive", command: "sudo --user root rm -rf /tmp/build", rule: "rm_recursive"},
		{name: "sudo group rm recursive", command: "sudo -g wheel rm -rf /tmp/build", rule: "rm_recursive"},
		{name: "sudo directory rm recursive", command: "sudo -D /tmp rm -rf /tmp/build", rule: "rm_recursive"},
		{name: "sudo command timeout rm recursive", command: "sudo -T 5 rm -rf /tmp/build", rule: "rm_recursive"},
		{name: "sudo close from rm recursive", command: "sudo -C 3 rm -rf /tmp/build", rule: "rm_recursive"},
		{name: "sudo chroot rm recursive", command: "sudo -R / rm -rf /tmp/build", rule: "rm_recursive"},
		{name: "sudo separator rm recursive", command: "sudo -- rm -rf /tmp/build", rule: "rm_recursive"},
		{name: "rm critical path", command: "rm /etc/passwd", rule: "rm_critical_path"},
		{name: "rm quoted critical path fragment", command: `rm /"etc"/passwd`, rule: "rm_critical_path"},
		{name: "find delete", command: "find /tmp -type f -delete", rule: "find_delete"},
		{name: "path find delete", command: "/usr/bin/find /tmp -delete", rule: "find_delete"},
		{name: "find quoted delete", command: `find /tmp "-delete"`, rule: "find_delete"},
		{name: "mkfs", command: "mkfs.ext4 /dev/sdb", rule: "filesystem_wipe"},
		{name: "path mkfs", command: "/sbin/mkfs.ext4 /dev/sdb", rule: "filesystem_wipe"},
		{name: "wipefs", command: "wipefs -a /dev/sdb", rule: "filesystem_wipe"},
		{name: "shred", command: "shred -u /etc/passwd", rule: "filesystem_wipe"},
		{name: "dd device output", command: "dd if=/dev/zero of=/dev/sda bs=1M", rule: "dd_dangerous_output"},
		{name: "dd absolute output", command: "dd if=/tmp/input of=/etc/passwd", rule: "dd_dangerous_output"},
		{name: "dd quoted output fragment", command: `dd if=/tmp/x of=/"dev"/sda`, rule: "dd_dangerous_output"},
		{name: "truncate redirect", command: ": > /etc/passwd", rule: "dangerous_redirect"},
		{name: "truncate redirect without space", command: ": >/etc/passwd", rule: "dangerous_redirect"},
		{name: "overwrite redirect", command: "cat /tmp/body > /var/log/app.log", rule: "dangerous_redirect"},
		{name: "overwrite redirect attached to source", command: "cat x>/var/log/app.log", rule: "dangerous_redirect"},
		{name: "stderr redirect attached", command: "cat x 2>/etc/passwd", rule: "dangerous_redirect"},
		{name: "redirect quoted target fragment", command: `cat x >/"etc"/passwd`, rule: "dangerous_redirect"},
		{name: "clobber redirect", command: ": >| /etc/passwd", rule: "dangerous_redirect"},
		{name: "attached clobber redirect", command: "cat x>|/etc/passwd", rule: "dangerous_redirect"},
		{name: "stderr clobber redirect", command: "cat x 2>|/etc/passwd", rule: "dangerous_redirect"},
		{name: "arbitrary fd redirect", command: ": 3> /etc/passwd", rule: "dangerous_redirect"},
		{name: "arbitrary fd clobber redirect", command: ": 3>| /etc/passwd", rule: "dangerous_redirect"},
		{name: "chmod recursive", command: "chmod -R 777 /etc", rule: "recursive_permission"},
		{name: "rm long recursive", command: "rm --recursive /tmp/build", rule: "rm_recursive"},
		{name: "chmod long recursive", command: "chmod --recursive 777 /etc", rule: "recursive_permission"},
		{name: "chmod quoted recursive", command: `chmod "-R" 777 /etc`, rule: "recursive_permission"},
		{name: "chown recursive", command: "chown -R root:root /var", rule: "recursive_permission"},
		{name: "chgrp recursive", command: "chgrp -R root /srv", rule: "recursive_permission"},
		{name: "compound", command: "pwd && rm -rf /tmp/build", rule: "rm_recursive"},
		{name: "newline compound", command: "pwd\nrm -rf /tmp/build", rule: "rm_recursive"},
		{name: "background compound", command: "sleep 1 & rm -rf /tmp/build", rule: "rm_recursive"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := CheckCommand(test.command)
			if !got.Dangerous {
				t.Fatalf("CheckCommand(%q) did not report danger", test.command)
			}
			if got.Rule != test.rule {
				t.Fatalf("CheckCommand(%q).Rule = %q, want %q", test.command, got.Rule, test.rule)
			}
			if got.Message == "" {
				t.Fatalf("CheckCommand(%q).Message is empty", test.command)
			}
		})
	}
}

func TestCheckCommandAllowsSafeCommands(t *testing.T) {
	tests := []string{
		`echo "rm -rf /"`,
		"rm file.tmp",
		"rm --force file.tmp",
		"rm --dir emptydir",
		"ls -la /etc",
		"cat /etc/passwd",
		"chmod --reference=/tmp/mode /etc/file",
		"grep root /etc/passwd",
		"tail -n 50 /var/log/syslog",
		"journalctl -p warning",
		"printf '> /etc/passwd\n'",
	}

	for _, command := range tests {
		t.Run(command, func(t *testing.T) {
			got := CheckCommand(command)
			if got.Dangerous {
				t.Fatalf("CheckCommand(%q) = %#v, want safe", command, got)
			}
		})
	}
}
