# broot shell helper. Managed by Rover (Ansible).
# Source this from the shell rc to get the `br` function, which launches broot
# and executes the command it produces (e.g. cd into the chosen directory).
# Named `br` (the standard broot function), not `brt`.
function br {
    local cmd cmd_file code
    cmd_file=$(mktemp)
    if broot --outcmd "$cmd_file" "$@"; then
        cmd=$(<"$cmd_file")
        command rm -f "$cmd_file"
        eval "$cmd"
    else
        code=$?
        command rm -f "$cmd_file"
        return "$code"
    fi
}
