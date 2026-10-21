# Sandbox per tab

Status: design + a working implementation of the two building blocks
(`src/sandbox.rs`). The renderer is not isolated yet: that needs the
process split described in "Phases" below.

## Threat model

A tab renders untrusted content: HTML, CSS and JavaScript coming from the
network, parsed by lexbor, styled by Stylo, laid out by Taffy, painted by
Vello and executed by QuickJS. Any of those is a potential remote code
execution bug, and a bug in the renderer runs with the privileges of the
user who started the browser.

What the sandbox has to protect:

| Asset | Threat | Protection |
| --- | --- | --- |
| The user's files | A renderer exploit reads or writes them | Process isolation (read-only mounts, private `/tmp`) |
| The browser process | A renderer exploit takes over the browser | The renderer runs in its own process; the browser only exchanges structured messages with it |
| Other tabs | One tab's exploit reaches the others | One process per tab, or one sandboxed renderer process for all tabs |
| The user's cookies and history | A renderer exploit reads them | They live in the browser process, never in the renderer |
| The kernel | A renderer exploit loads a module or pokes /proc | The seccomp filter denies those syscalls |

What a seccomp filter alone **cannot** do: it is not a memory sandbox. A
renderer that is exploited keeps the privileges of its process, so it can
still read whatever the process can read. That is why the filter and the
process split belong together.

## The three mechanisms

| | seccomp | Landlock | bubblewrap |
| --- | --- | --- | --- |
| Restricts | syscalls | filesystem paths | namespaces, mounts, cgroups, and optionally syscalls |
| Needs | kernel >= 3.17 | kernel >= 5.13 | user namespaces enabled, `bwrap` installed |
| Privileges | none, `no_new_privs` is enough | none | unprivileged user namespaces |
| Per process | yes | yes (per thread) | no, it is a wrapper around a child |
| Granularity | syscall names (and arguments) | path + access rights | whole namespaces |
| Cost | a few microseconds at load | a few rules per path | a fork plus the namespace setup |
| In this machine | available | kernel 7.2, available | `bwrap` installed and working |

The choice made here: **seccomp for the syscalls, bubblewrap for the
process and the filesystem, Landlock as a possible refinement** for the
filesystem when the process split exists. Landlock is not used yet: inside
bubblewrap the filesystem is already read-only, and Landlock only becomes
interesting when the renderer must write to a controlled place (a cache
directory) without giving it the whole home directory.

## Phases

### L1 — the syscall filter (implemented)

`src/sandbox.rs` builds a seccomp filter with the system libseccomp and
installs it in the current process:

- `Policy::DenyDangerous`: everything is allowed except the syscalls that
  break out of a renderer: process creation (`execve`, `execveat`),
  tracing other processes (`ptrace`, `process_vm_readv`), filesystem and
  namespace changes (`mount`, `pivot_root`, `setns`, `unshare`), privileged
  kernel interfaces (`bpf`, `perf_event_open`, `init_module`, `keyctl`,
  `userfaultfd`, `io_uring_*`), and the calls that change the execution
  domain (`personality`, `setuid`, `setreuid`, ...).
- `Policy::AllowOnlyRenderer`: only the syscalls a renderer needs are
  allowed, everything else fails with `EPERM`. It is opt-in, because the
  list has to follow the engine: a missing syscall shows up as a strange
  failure in the renderer, not as a compile error.

The filter is installed with `no_new_privs` and `tsync`, so it applies to
every thread and cannot be lifted. It is a one-way door: that is why the
tests run it in a child process, never in the test process itself.

`Policy::DenyDangerous` is safe to apply to the current engine today, since
it keeps the network (`socket`) available. `Policy::AllowOnlyRenderer` is
not: it denies `socket`, so it only makes sense in a renderer that never
fetches by itself.

### L2 — the renderer in its own process (designed)

The renderer becomes a child process started with `sandbox::spawn_isolated`:

```rust
let child = sandbox::spawn_isolated(Path::new("blitz-renderer"))?;
```

The child gets, through bubblewrap:

- a read-only root (`--ro-bind / /`), so it cannot change the system;
- a private `/tmp` (`--tmpfs /tmp`), so it cannot read other tabs' scratch
  space;
- its own pid, ipc, uts, cgroup and network namespaces
  (`--unshare-all`): it cannot see the other tabs, and it has **no network
  at all**;
- `--die-with-parent` and `--new-session`, so it cannot outlive the browser
  or keep the terminal.

Inside that child, the L1 filter is installed before the first byte of
untrusted content is parsed. The order matters: the filter is installed
first, then the page is parsed.

### L3 — the browser keeps the network (designed)

With the renderer isolated, the browser process becomes the only one that
talks to the network, which is also where the cookie jar and the history
live. The protocol between the two is small and structured:

```
renderer -> browser:  Load { url }
browser  -> renderer:  Loaded { url, final_url, mime, body }
renderer -> browser:  Request { url, kind }     (css, image, font)
browser  -> renderer:  Resource { body }
```

The renderer never opens a socket, so a renderer exploit cannot exfiltrate
anything except through the messages the protocol allows. The protocol is
the place where the remaining policy lives: which hosts are reachable, how
big a response may be, which request headers are allowed.

## The policy in detail

`DANGEROUS_SYSCALLS` is the denylist of the permissive policy.
`RENDERER_SYSCALLS` is the allowlist of the strict one: file and memory
calls, thread and synchronisation calls, timers, waiting, signals and
process bookkeeping. What it leaves out on purpose:

- `socket`, `connect`, `sendto`, `recvfrom`: the renderer does not fetch.
- `execve`, `execveat`: the renderer does not run programs.
- `ptrace`, `process_vm_readv`: the renderer does not inspect other
  processes.
- `open_by_handle_at`, `name_to_handle_at`: privileged file access.
- `mount`, `setns`, `unshare`: the renderer does not create namespaces.

Residual risk, stated plainly: `mmap`/`mprotect` are allowed, so a renderer
exploit can still write and execute memory. That is expected: the filter
is about what the process may ask the kernel for, not about memory safety.
The real boundary is the process split of L2.

## What was measured

With the tests in `tests/sandbox.rs`:

- The filter installs (`/proc/self/status` reports `Seccomp: 2`) and
  `execve` then fails: the child cannot start `/bin/true` any more.
- The sandboxed child runs, prints what it was asked to print, and exits
  cleanly.
- The sandboxed child has **no route** (`/proc/net/route` holds only its
  header) and sees none of the host interfaces.

## Not done yet

- The renderer is not a separate process yet: `blitz-tabs` still renders
  in the browser process, so the filter is not applied to anything. The
  wiring needs `src/bin/blitz-tabs.rs` (the renderer binary) and the
  protocol of L3.
- `Policy::AllowOnlyRenderer` has not been run against a real page; its
  list needs a pass with the engine under `strace` to see what a page load
  really asks for.
- Landlock is not used, see above.
- There is no seccomp filter for the browser process itself; the browser is
  the trusted party by design.
