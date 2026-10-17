package selfname

// ShellWords are the names a shell keeps for itself: the reserved words and
// builtins of zsh, bash, fish and POSIX sh (D3). A command by one of these
// names is never run: the shell's own wins, or the line doesn't parse.
// Entries that fail the name syntax ("!", "[[") are kept so the list says
// what the shells say; Check refuses them on syntax first.
//
// Sources: zsh's $reswords and builtins (zshbuiltins(1), plus private and
// the zsh/* modules' builtins every setup loads), bash's compgen -k and
// compgen -b, fish's builtin -n and its shipped functions with plain names,
// and POSIX.1-2017's reserved words, special builtins and intrinsic
// utilities.
var ShellWords = []string{
	"!", ".", ":", "[", "[[", "]]", "{", "}",
	"abbr", "alias", "and", "argparse", "autoload",
	"begin", "bg", "bind", "bindkey", "block", "break", "breakpoint", "builtin", "bye",
	"caller", "case", "cd", "cdh", "chdir", "command", "commandline", "compadd",
	"comparguments", "compcall", "compctl", "compdef", "compdescribe", "compfiles",
	"compgen", "compgroups", "compinit", "complete", "compopt", "compquote",
	"comptags", "comptry", "compvalues", "contains", "continue", "coproc", "count",
	"declare", "dirh", "dirs", "disable", "disown", "do", "done",
	"echo", "elif", "else", "emit", "emulate", "enable", "end", "esac", "eval",
	"exec", "exit", "export",
	"false", "fc", "fg", "fi", "float", "for", "foreach", "funced", "funcsave",
	"function", "functions",
	"getln", "getopts",
	"hash", "help", "history",
	"if", "in", "integer", "isatty",
	"jobs",
	"kill",
	"let", "limit", "local", "log", "logout",
	"mapfile", "math",
	"newgrp", "nextd", "nocorrect", "noglob", "not",
	"or",
	"path", "popd", "prevd", "print", "printf", "private", "psub", "pushd", "pushln", "pwd",
	"r", "random", "read", "readarray", "readonly", "realpath", "rehash", "repeat", "return",
	"sched", "select", "set", "setopt", "shift", "shopt", "source", "status", "string",
	"suspend", "switch",
	"test", "then", "time", "times", "trap", "true", "ttyctl", "type", "typeset",
	"ulimit", "umask", "unalias", "unfunction", "unhash", "unlimit", "unset", "unsetopt",
	"until",
	"vared",
	"wait", "whence", "where", "which", "while",
	"zcompile", "zformat", "zle", "zmodload", "zparseopts", "zregexparse", "zstyle",
}

// ToolNames are the programs homebot runs, or has its scripts and the
// programs it starts run, by name on PATH (D3). A link by one of these names
// in ~/.local/bin, early on PATH, would make homebot run itself instead.
//
// It is written by hand: proc.Run and proc.RunAttached take an argv that
// wrappers build far from the call, so no source scan finds them all. A new
// program goes here; the comments on proc.Run, proc.RunAttached,
// proc.LookPath and proc.HasCommand say so, and cmd/homebot's TestMain fails
// when the test suite runs a program missing from it (the program log).
var ToolNames = []string{
	"age", "age-keygen", "age-plugin-batchpass", // secrets: age's tools, and a plugin its library runs from PATH
	"apt", "apt-cache", "apt-get", "dpkg-query", // the apt and apt-repos actions
	"bash", "fish", "sh", "zsh", // shells: actions, exports, the shell integration
	"brew",                 // the brew action; release updates
	"cat",                  // cleanup's fzf preview
	"cosign", "curl", "go", // release updates and the front door
	"fc-cache",       // the fonts action
	"flatpak",        // the flatpak action
	"fzf", "lazygit", // cleanup's choosers and merges
	"gdbus", "notify-send", "osascript", // notifications
	"gh", "glab", // remote
	"git", "gitleaks", // everywhere; the guard
	"gpg",                                // git's commit signing
	"launchctl", "loginctl", "systemctl", // the helper
	"ssh", "ssh-keygen", // git over ssh; keys
	"sudo", // the apt actions
}
