# Host setup

sandpitd itself needs no root. Two optional, one-time steps do: guest networking and the
reflink volume. Both install boot units, so they survive a reboot.

## Guest networking

```sh
make netd                        # optional: the helper that makes network policies enforceable
sudo ./scripts/setup-host.sh     # bridge (10.209.0.0/16) + tap pool + NAT/isolation rules + boot units
```

Sprites get outbound internet but cannot reach each other, the host, or private/LAN/tailnet
ranges. Without the setup sprites simply have no NIC; exec, checkpoints, sprite URLs and the
TCP proxy still work because they travel over vsock.

The names the script gives the host are still wisp's, the project sandpit was cut from:
bridge `msbr0`, taps `mstap*`, nftables table `inet wisp`, boot units `wisp-net` and
`wisp-netd`, the helper (`sandpit-netd`, built by `make netd`) installed as
`/usr/local/sbin/wisp-netd` with its socket at `/run/wisp/netd.sock`, and the `WISP_*`
variables below. That is deliberate: it lets sandpitd take over a wisp `sandboxd` install in
place, network and all ([operations](operations.md#beside-wisp)). A later migration renames
them.

What the script knows about because it bit us:
- It refuses a subnet that overlaps an existing route (`WISP_NET_PREFIX` picks another
  /16). podman owns 10.88/16 by default, and an overlap silently steals all return traffic.
  sandpitd reads the network back off the bridge, so the script is the only place it is set.
- If ufw is active it adds `ufw route allow in on msbr0` and input allowances for the two
  policy ports: ufw's policies are DROP, and a drop in any netfilter table is final.
- `--print-rules` shows the nftables ruleset without root; `--remove` undoes everything.

### A second network pool

One daemon owns a pool of taps at a time. To run a second networked daemon on the same
host (a test stack beside the main one, or sandpitd beside a wispd), give it a pool of its own:

```sh
make netd
sudo WISP_POOL=1 ./scripts/setup-host.sh
```

Pool N is a full copy of the above under other names: bridge `msbrN` on 10.(209+N).0.0/16
(`WISP_NET_PREFIX` still overrides), taps `msNtap*`, table `inet wispN`, boot units
`wisp-netN` and `wisp-netdN`, the helper's socket at `/run/wispN/netd.sock`. Pool 0, the
default, keeps the names above, so nothing about an existing install changes and the two
never touch each other's devices, rules or helper. The policy listeners use the same ports
on each pool's own bridge address. Start the second daemon with `--net-pool 1`;
`WISP_POOL=1 ./scripts/setup-host.sh --remove` takes the pool away again.

## Instant clones

```sh
sudo apt install xfsprogs
sudo ./scripts/setup-storage.sh      # stop sandpitd first; SPRITE_VOLUME_GB=40 by default
```

Puts the sprite directory on a loop-mounted XFS volume with reflinks, so creating a sprite,
taking a checkpoint and restoring one are instant and share disk blocks until written
(measured: create 13 ms, checkpoint 2 ms, restore 69 ms; six 20 GB images in 1.7 GB). Warm
snapshots live on the volume too, one per suspended sprite, taking what the guest was using
but briefly more than its RAM size while written, so size it for both; suspend is a little slower through the loop device (~1.6 s vs ~1.2 s). The
script migrates existing sprites, never sizes the volume beyond what the host disk can hold,
and `--remove` moves everything back. sandpitd needs no configuration: it probes the
filesystem at startup and logs which mode it is in.

To give sprites more room later, grow the volume, and optionally move its image to a disk
with more space (the image is one file; by default it sits in the data directory):

```sh
systemctl --user stop sandpit   # suspends every sprite; they resume warm afterwards
sudo SPRITE_VOLUME_GB=300 SPRITE_VOLUME_IMAGE=/data/sandpit/sprites.xfs \
     ./scripts/setup-storage.sh --grow
systemctl --user start sandpit
```

Either variable can be left out: `SPRITE_VOLUME_GB` alone grows the image where it is,
`SPRITE_VOLUME_IMAGE` alone moves it. It only ever grows, since XFS cannot shrink, and every
sprite is kept: the filesystem is extended in place, a move copies the image and removes the
old one only once the copy is mounted, and the boot unit is rewritten to wait for the new
disk. The size is refused if the target disk could not hold it plus 5 GB, for the same reason
as at creation: an image that outgrows its disk fails with I/O errors inside guests.
