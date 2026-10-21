//! Sandbox for the renderer.
//!
//! Two independent layers, both optional:
//!
//! - A **seccomp** filter that restricts the syscalls a process may make. It
//!   is installed in the renderer process before any untrusted content is
//!   parsed, and it cannot be undone afterwards.
//! - A **bubblewrap** wrapper that runs a child in private namespaces with a
//!   read-only filesystem and no network.
//!
//! The filter is a syscall filter, not a memory sandbox: a renderer that is
//! exploited can still use whatever the allowed syscalls allow. Isolating the
//! renderer in its own process is what turns the filter into a boundary, so
//! the two layers are meant to be used together.
//!
//! The design notes live in `docs/sandbox.md`.

use std::ffi::CString;
use std::io;
use std::os::raw::{c_char, c_int, c_uint, c_void};
use std::path::Path;
use std::process::{Child, Command};

// ---------- libseccomp ----------

/// The seccomp actions and attributes the sandbox uses.
mod ffi_consts {
    use super::c_uint;

    /// `SCMP_ACT_ERRNO(EPERM)`.
    pub const SCMP_ACT_ERRNO_EPERM: c_uint = 0x0005_0000 | 1;
    /// `SCMP_ACT_ALLOW`.
    pub const SCMP_ACT_ALLOW: c_uint = 0x7fff_0000;
    /// `SCMP_ARCH_NATIVE`.
    pub const SCMP_ARCH_NATIVE: c_uint = 0;
    /// `SCMP_FLTATR_CTL_NNP`: set `no_new_privs` when the filter loads.
    pub const SCMP_FLTATR_CTL_NNP: c_uint = 3;
    /// `SCMP_FLTATR_CTL_TSYNC`: apply the filter to every thread.
    pub const SCMP_FLTATR_CTL_TSYNC: c_uint = 4;
}

#[link(name = "seccomp")]
extern "C" {
    fn seccomp_init(def_action: c_uint) -> *mut c_void;
    fn seccomp_release(ctx: *mut c_void);
    fn seccomp_rule_add(
        ctx: *mut c_void,
        action: c_uint,
        syscall: c_int,
        arg_cnt: c_uint,
        ...
    ) -> c_int;
    fn seccomp_load(ctx: *mut c_void) -> c_int;
    fn seccomp_syscall_resolve_name(name: *const c_char) -> c_int;
    fn seccomp_attr_set(ctx: *mut c_void, attr: c_uint, value: c_uint) -> c_int;
    fn seccomp_arch_add(ctx: *mut c_void, arch: c_uint) -> c_int;
}

/// What the sandbox does with a syscall.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Verdict {
    /// The syscall may run.
    Allow,
    /// The syscall fails with `EPERM`.
    Deny,
}

/// How strict the syscall filter is.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Policy {
    /// Everything is allowed except the syscalls that break out of the
    /// renderer: process creation, tracing, namespaces, kernel modules and
    /// the like. Safe to apply to the current engine.
    DenyDangerous,
    /// Only the syscalls a renderer needs are allowed; everything else fails
    /// with `EPERM`. Stricter, but the list has to be kept in step with the
    /// engine, so it is opt-in.
    AllowOnlyRenderer,
}

/// The syscalls that break out of a renderer process.
pub const DANGEROUS_SYSCALLS: &[&str] = &[
    // Running other programs.
    "execve",
    "execveat",
    // Watching or changing other processes.
    "ptrace",
    "process_vm_readv",
    "process_vm_writev",
    // Changing the filesystem or the machine.
    "mount",
    "umount2",
    "pivot_root",
    "chroot",
    "setns",
    "unshare",
    "swapon",
    "swapoff",
    "reboot",
    "settimeofday",
    "clock_settime",
    "adjtimex",
    // Privileged kernel interfaces.
    "bpf",
    "perf_event_open",
    "init_module",
    "finit_module",
    "delete_module",
    "kexec_load",
    "kexec_file_load",
    "keyctl",
    "add_key",
    "request_key",
    "open_by_handle_at",
    "name_to_handle_at",
    "userfaultfd",
    "io_uring_setup",
    "io_uring_enter",
    "io_uring_register",
    // Changing the execution domain or the credentials.
    "personality",
    "setuid",
    "setgid",
    "setreuid",
    "setregid",
    "setresuid",
    "setresgid",
    "acct",
    "quotactl",
];

/// The syscalls a renderer needs, for [`Policy::AllowOnlyRenderer`].
pub const RENDERER_SYSCALLS: &[&str] = &[
    // Files and memory.
    "read",
    "write",
    "readv",
    "writev",
    "pread64",
    "pwrite64",
    "preadv",
    "pwritev",
    "open",
    "openat",
    "openat2",
    "close",
    "fstat",
    "newfstatat",
    "statx",
    "lseek",
    "ftruncate",
    "fadvise64",
    "getdents64",
    "ioctl",
    "fcntl",
    "dup",
    "dup2",
    "dup3",
    "pipe2",
    "mmap",
    "mprotect",
    "munmap",
    "mremap",
    "madvise",
    "brk",
    // Threads and synchronisation.
    "clone",
    "clone3",
    "futex",
    "futex_waitv",
    "set_robust_list",
    "rseq",
    "set_tid_address",
    "sched_yield",
    "sched_getaffinity",
    "sched_getparam",
    "sched_getscheduler",
    "nanosleep",
    "clock_nanosleep",
    // Time.
    "clock_gettime",
    "clock_getres",
    "gettimeofday",
    // Waiting.
    "epoll_create1",
    "epoll_ctl",
    "epoll_wait",
    "epoll_pwait",
    "epoll_pwait2",
    "eventfd",
    "eventfd2",
    "timerfd_create",
    "timerfd_settime",
    "timerfd_gettime",
    "poll",
    "ppoll",
    "select",
    "pselect6",
    // Signals.
    "rt_sigaction",
    "rt_sigprocmask",
    "rt_sigreturn",
    "rt_sigtimedwait",
    "sigaltstack",
    "kill",
    "tgkill",
    "pause",
    // Process bookkeeping.
    "getpid",
    "gettid",
    "getppid",
    "getuid",
    "geteuid",
    "getgid",
    "getegid",
    "getgroups",
    "uname",
    "sysinfo",
    "getrusage",
    "getrlimit",
    "prlimit64",
    "setrlimit",
    "getrandom",
    "restart_syscall",
    "exit",
    "exit_group",
    "wait4",
    "waitid",
];

impl Policy {
    /// The verdict for one syscall name.
    pub fn verdict(&self, syscall: &str) -> Verdict {
        match self {
            Policy::DenyDangerous => {
                if DANGEROUS_SYSCALLS.contains(&syscall) {
                    Verdict::Deny
                } else {
                    Verdict::Allow
                }
            }
            Policy::AllowOnlyRenderer => {
                if RENDERER_SYSCALLS.contains(&syscall) {
                    Verdict::Allow
                } else {
                    Verdict::Deny
                }
            }
        }
    }

    /// The syscalls this policy allows.
    pub fn allowed_syscalls(&self) -> &'static [&'static str] {
        match self {
            Policy::DenyDangerous => &[],
            Policy::AllowOnlyRenderer => RENDERER_SYSCALLS,
        }
    }

    /// The syscalls this policy denies.
    pub fn denied_syscalls(&self) -> &'static [&'static str] {
        match self {
            Policy::DenyDangerous => DANGEROUS_SYSCALLS,
            Policy::AllowOnlyRenderer => &[],
        }
    }

    /// The action the filter takes when no rule matches.
    fn default_action(&self) -> c_uint {
        match self {
            Policy::DenyDangerous => ffi_consts::SCMP_ACT_ALLOW,
            Policy::AllowOnlyRenderer => ffi_consts::SCMP_ACT_ERRNO_EPERM,
        }
    }
}

/// Install the syscall filter in the current process.
///
/// This cannot be undone: the filter stays for the life of the process, and
/// it is applied to every thread. Call it in the renderer process, before any
/// untrusted content is parsed.
pub fn install(policy: Policy) -> Result<(), String> {
    unsafe {
        let ctx = seccomp_init(policy.default_action());
        if ctx.is_null() {
            return Err("seccomp_init failed".to_string());
        }
        let built = build_rules(ctx, policy);
        if let Err(err) = built {
            seccomp_release(ctx);
            return Err(err);
        }
        // `no_new_privs` keeps the filter from being lifted, and `tsync`
        // applies it to the threads that already exist.
        seccomp_attr_set(ctx, ffi_consts::SCMP_FLTATR_CTL_NNP, 1);
        seccomp_attr_set(ctx, ffi_consts::SCMP_FLTATR_CTL_TSYNC, 1);
        if seccomp_load(ctx) != 0 {
            seccomp_release(ctx);
            return Err("seccomp_load failed".to_string());
        }
        seccomp_release(ctx);
        Ok(())
    }
}

unsafe fn build_rules(ctx: *mut c_void, policy: Policy) -> Result<(), String> {
    seccomp_arch_add(ctx, ffi_consts::SCMP_ARCH_NATIVE);
    for name in policy.allowed_syscalls() {
        add_rule(ctx, ffi_consts::SCMP_ACT_ALLOW, name)?;
    }
    for name in policy.denied_syscalls() {
        add_rule(ctx, ffi_consts::SCMP_ACT_ERRNO_EPERM, name)?;
    }
    Ok(())
}

unsafe fn add_rule(ctx: *mut c_void, action: c_uint, name: &str) -> Result<(), String> {
    let c_name = CString::new(name).map_err(|_| format!("bad syscall name {name}"))?;
    let number = seccomp_syscall_resolve_name(c_name.as_ptr());
    if number < 0 {
        // The syscall does not exist on this architecture: nothing to filter.
        return Ok(());
    }
    if seccomp_rule_add(ctx, action, number, 0) != 0 {
        return Err(format!("seccomp_rule_add failed for {name}"));
    }
    Ok(())
}

/// The seccomp mode of this process: 0 disabled, 1 strict, 2 filter.
pub fn seccomp_mode() -> Option<u8> {
    let status = std::fs::read_to_string("/proc/self/status").ok()?;
    for line in status.lines() {
        if let Some(value) = line.strip_prefix("Seccomp:") {
            return value.trim().parse().ok();
        }
    }
    None
}

/// Whether a syscall filter is active in this process.
pub fn is_active() -> bool {
    seccomp_mode() == Some(2)
}

/// Whether bubblewrap is available on this machine.
pub fn isolation_available() -> bool {
    if let Ok(path) = std::env::var("PATH") {
        for directory in path.split(':') {
            if directory.is_empty() {
                continue;
            }
            if Path::new(directory).join("bwrap").is_file() {
                return true;
            }
        }
    }
    for candidate in ["/usr/bin/bwrap", "/bin/bwrap", "/usr/local/bin/bwrap"] {
        if Path::new(candidate).is_file() {
            return true;
        }
    }
    false
}

/// The command that runs `program` inside the bubblewrap sandbox.
///
/// The child gets a read-only filesystem, a private `/tmp`, its own pid,
/// ipc, uts, cgroup and network namespaces, and dies with the parent. It has
/// no network: the browser process does the fetching and hands the bytes
/// over.
pub fn isolated_command(program: &Path) -> Command {
    let mut command = Command::new("bwrap");
    command
        .arg("--ro-bind")
        .arg("/")
        .arg("/")
        .arg("--dev")
        .arg("/dev")
        .arg("--proc")
        .arg("/proc")
        .arg("--tmpfs")
        .arg("/tmp")
        .arg("--unshare-all")
        .arg("--die-with-parent")
        .arg("--new-session")
        .arg(program);
    command
}

/// Run `program` inside the bubblewrap sandbox.
pub fn spawn_isolated(program: &Path) -> io::Result<Child> {
    isolated_command(program).spawn()
}
