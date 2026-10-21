//! T10 — sandbox: the seccomp syscall filter and the bubblewrap isolation.
//!
//! The filter is installed in a child process: it cannot be undone, so the
//! test binary re-runs itself as a probe and the parent checks what the child
//! could and could not do.

use std::path::Path;
use std::process::Command;

use browser_lite::sandbox::{
    install, is_active, isolation_available, isolated_command, seccomp_mode, Policy, Verdict,
    DANGEROUS_SYSCALLS, RENDERER_SYSCALLS,
};

/// The name of the test that runs inside the sandboxed child.
const PROBE_TEST: &str = "sandbox_probe_child";
/// The environment variable that tells the probe what to do.
const PROBE_ENV: &str = "BLITZ_SANDBOX_PROBE";

#[test]
fn the_dangerous_syscalls_are_denied() {
    for syscall in DANGEROUS_SYSCALLS {
        assert_eq!(
            Policy::DenyDangerous.verdict(syscall),
            Verdict::Deny,
            "{syscall} must be denied"
        );
    }
    assert_eq!(Policy::DenyDangerous.verdict("read"), Verdict::Allow);
    assert_eq!(Policy::DenyDangerous.verdict("write"), Verdict::Allow);
}

#[test]
fn the_strict_policy_only_allows_the_renderer_syscalls() {
    for syscall in RENDERER_SYSCALLS {
        assert_eq!(
            Policy::AllowOnlyRenderer.verdict(syscall),
            Verdict::Allow,
            "{syscall} must be allowed"
        );
    }
    for syscall in DANGEROUS_SYSCALLS {
        assert_eq!(
            Policy::AllowOnlyRenderer.verdict(syscall),
            Verdict::Deny,
            "{syscall} must be denied"
        );
    }
    // The renderer does not do the fetching, so it does not open sockets.
    assert_eq!(Policy::AllowOnlyRenderer.verdict("socket"), Verdict::Deny);
    assert_eq!(Policy::DenyDangerous.verdict("socket"), Verdict::Allow);
}

#[test]
fn the_policies_cover_the_syscalls_they_claim() {
    for syscall in DANGEROUS_SYSCALLS {
        assert!(Policy::DenyDangerous.denied_syscalls().contains(syscall));
    }
    assert!(Policy::DenyDangerous.allowed_syscalls().is_empty());
    assert_eq!(
        Policy::AllowOnlyRenderer.allowed_syscalls().len(),
        RENDERER_SYSCALLS.len()
    );
    assert!(Policy::AllowOnlyRenderer.denied_syscalls().is_empty());
}

#[test]
fn the_process_starts_without_a_filter() {
    // The parent is not sandboxed; the child is.
    assert_eq!(seccomp_mode(), Some(0));
    assert!(!is_active());
}

/// Runs in the child process when `BLITZ_SANDBOX_PROBE` is set.
#[test]
fn sandbox_probe_child() {
    let Ok(mode) = std::env::var(PROBE_ENV) else {
        // Not the child: nothing to do.
        return;
    };

    match mode.as_str() {
        "install" => {
            install(Policy::DenyDangerous).expect("the filter must install");
            println!("seccomp mode: {:?}", seccomp_mode());
            // The filter is active, and process creation is blocked.
            let spawn = std::process::Command::new("/bin/true").status();
            match spawn {
                Ok(_) => println!("execve: allowed"),
                Err(err) => println!("execve: blocked ({err})"),
            }
            std::process::exit(0);
        }
        other => {
            println!("unknown probe mode {other}");
            std::process::exit(2);
        }
    }
}

#[test]
fn the_seccomp_filter_blocks_process_creation() {
    let executable = std::env::current_exe().expect("test executable");
    let output = Command::new(executable)
        .arg("--exact")
        .arg(PROBE_TEST)
        .arg("--nocapture")
        .env(PROBE_ENV, "install")
        .output()
        .expect("run the probe");

    let stdout = String::from_utf8_lossy(&output.stdout);
    assert!(
        stdout.contains("seccomp mode: Some(2)"),
        "the filter must be active, stdout was: {stdout}"
    );
    assert!(
        stdout.contains("execve: blocked"),
        "process creation must be blocked, stdout was: {stdout}"
    );
}

#[test]
fn bubblewrap_runs_a_child_without_a_network() {
    if !isolation_available() {
        eprintln!("bubblewrap is not installed: skipping");
        return;
    }
    let output = isolated_command(Path::new("/bin/echo"))
        .arg("sandbox-ok")
        .output()
        .expect("run the sandboxed child");

    assert!(
        output.status.success(),
        "the sandboxed child failed: {}",
        String::from_utf8_lossy(&output.stderr)
    );
    assert_eq!(String::from_utf8_lossy(&output.stdout).trim(), "sandbox-ok");
}

#[test]
fn bubblewrap_hides_the_network() {
    if !isolation_available() {
        eprintln!("bubblewrap is not installed: skipping");
        return;
    }
    // The namespace has no route at all, and none of the host interfaces.
    let output = isolated_command(Path::new("/bin/sh"))
        .arg("-c")
        .arg("cat /proc/net/route; echo ---; cat /proc/net/dev")
        .output()
        .expect("run the sandboxed child");

    let text = String::from_utf8_lossy(&output.stdout);
    let (routes, interfaces) = text.split_once("---").expect("both parts");
    let route_lines = routes.lines().filter(|line| !line.trim().is_empty()).count();
    assert_eq!(route_lines, 1, "the sandbox must have no route: {routes}");
    for interface in ["eth0", "wlan0", "enp", "wlp"] {
        assert!(
            !interfaces.contains(interface),
            "the sandbox must not see {interface}: {interfaces}"
        );
    }
}
