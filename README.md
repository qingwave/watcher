Watch
=====

## Usage
Usage: ``Watch [-v] [-t]  [-p <path>] [-x <regexp>] <command>``

Watches for changes in a directory tree, and runs a command when
something changed. By default, the output goes to an acme win.

-t sends the output to the terminal instead of acme

-v enables verbose debugging output

-p <path> specifies the path to watch (if it is a directory then it watches recursively)

-x <regexp> specifies a regexp used to exclude files and directories from the watcher

-c <command> the command when file changes

## Multi path support
-p <path1>
-c <cmd1>
-x <exclude1> (the watcher use exclude files shoud rank ahead)
-p <path2>
-c <cmd2>
...

## Install
make
