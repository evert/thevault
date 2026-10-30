# /etc/profile.d/thevault.sh: show The Vault's daily news at login.
# Interactive terminals only, and never let the game slow down or break a login.
if [ -t 1 ] && command -v thevault >/dev/null 2>&1; then
    timeout 2 thevault motd 2>/dev/null
fi
