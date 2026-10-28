package cli

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// dotgenie commits as git_author from config.yml, a "Name <email>" string
// shared by every machine through the repo. It's passed to each commit with
// -c, so it never changes git's own config. Without it, git's identity on the
// machine is used; with neither, the commit is refused with the fix spelled
// out (git's own refusal is exit status 128, which says nothing).

var authorRe = regexp.MustCompile(`^\s*([^<>]*[^<>\s])\s*<([^<>\s]+@[^<>\s]+)>\s*$`)

// parseAuthor splits "Name <email>".
func parseAuthor(s string) (name, email string, ok bool) {
	m := authorRe.FindStringSubmatch(s)
	if m == nil {
		return "", "", false
	}
	return m[1], m[2], true
}

// gitIdentity is the identity git would commit as in dir, "Name <email>",
// or "" if git has none.
func gitIdentity(dir string) string {
	name, _ := git(dir, "config", "user.name")
	email, _ := git(dir, "config", "user.email")
	if name == "" || email == "" {
		return ""
	}
	return name + " <" + email + ">"
}

var errNoIdentity = errors.New("no identity for dotgenie's commits: add a line like\n" +
	"    git_author: Your Name <you@example.com>\n" +
	"  to config.yml in your dotfiles repo (every machine then has it), or set\n" +
	"  git config user.name and user.email on this machine")

// gitCommit commits what's staged in dir with msg, as author (from config.yml)
// if set, else as git's own identity. A failure carries git's message, or for
// want of an identity, errNoIdentity with the fix.
func gitCommit(dir, author, msg string) error {
	var args []string
	if author != "" {
		name, email, ok := parseAuthor(author)
		if !ok {
			return fmt.Errorf("git_author in config.yml should look like \"Your Name <you@example.com>\"; it's %q", author)
		}
		args = append(args, "-c", "user.name="+name, "-c", "user.email="+email)
	}
	args = append(args, "commit", "-q", "-m", msg)
	out, err := git(dir, args...)
	switch {
	case err == nil:
		return nil
	case noIdentityRe.MatchString(out):
		// Git found no identity (config, environment or a guess): say how to
		// fix it rather than pass on "exit status 128".
		return errNoIdentity
	case out == "":
		return fmt.Errorf("git commit: %w", err)
	default:
		return fmt.Errorf("git commit: %s", out)
	}
}

// noIdentityRe matches git's refusals for want of an identity.
var noIdentityRe = regexp.MustCompile(`(?i)author identity unknown|please tell me who you are|empty ident name|no email was given`)

// askAuthor asks once for git_author. Blank or unreadable input skips it.
func askAuthor() string {
	r := bufio.NewReader(os.Stdin)
	for range 3 {
		fmt.Print("Name and email for dotgenie's commits (Your Name <you@example.com>), blank to skip: ")
		line, err := r.ReadString('\n')
		line = strings.TrimSpace(line)
		if line == "" {
			return ""
		}
		if _, _, ok := parseAuthor(line); ok {
			return line
		}
		fmt.Println("  That doesn't look like: Your Name <you@example.com>")
		if err != nil {
			return ""
		}
	}
	return ""
}
