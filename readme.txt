# open tmux session
tmux list-session
tmux attach -t N 	# N is number of needed session
# if only one session exist
tmux attach
# for disconnect press Ctrl-B and D

# make sd readwrite
arm-sdtool /dev/mmcblkX unlock    # lsblk for find X
# make sd readonly
arm-sdtool /dev/mmcblkX lock

# disable motd service
systemctl disable motd
# stop
systemctl stop motd
# etc
systemctl enable motd
systemctl star motd

# same for barnard (voice client)
systemctl disable barnard
# and mumble (voice server)
systemctl disable mumble-server

# asot some motd but for assun motors
#		Password:      "admin",
#		TCPServerPort: 502,
#		WebServerPort: 1111,
#/etc/systemd/system/asot.service
#/usr/bin/asot
