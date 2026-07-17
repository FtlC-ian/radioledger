#[cfg(test)]
pub(crate) fn with_temp_home<F>(prefix: &str, test_fn: F)
where
    F: FnOnce(),
{
    use std::sync::{Mutex, OnceLock};

    static TEST_LOCK: OnceLock<Mutex<()>> = OnceLock::new();
    let _guard = TEST_LOCK.get_or_init(|| Mutex::new(())).lock().unwrap();

    let old_home = std::env::var("HOME").ok();
    let old_config_dir = std::env::var("RADIOLEDGER_CONFIG_DIR").ok();
    #[cfg(windows)]
    let old_userprofile = std::env::var("USERPROFILE").ok();
    #[cfg(windows)]
    let old_homedrive = std::env::var("HOMEDRIVE").ok();
    #[cfg(windows)]
    let old_homepath = std::env::var("HOMEPATH").ok();

    let temp_home = std::env::temp_dir().join(format!("{prefix}-{}", uuid::Uuid::new_v4()));
    let temp_config_dir = temp_home.join(".radioledger");
    std::fs::create_dir_all(&temp_config_dir).unwrap();
    std::env::set_var("HOME", &temp_home);
    std::env::set_var("RADIOLEDGER_CONFIG_DIR", &temp_config_dir);

    #[cfg(windows)]
    {
        use std::path::{Component, Prefix};

        std::env::set_var("USERPROFILE", &temp_home);

        let mut components = temp_home.components();
        match components.next() {
            Some(Component::Prefix(prefix_component)) => {
                match prefix_component.kind() {
                    Prefix::Disk(drive) | Prefix::VerbatimDisk(drive) => {
                        let drive_letter = (drive as char).to_ascii_uppercase();
                        std::env::set_var("HOMEDRIVE", format!("{drive_letter}:"));
                        let home_path = format!(
                            "\\{}",
                            components.as_path().display().to_string().replace('/', "\\")
                        );
                        std::env::set_var("HOMEPATH", home_path);
                    }
                    _ => {
                        std::env::remove_var("HOMEDRIVE");
                        std::env::remove_var("HOMEPATH");
                    }
                }
            }
            _ => {
                std::env::remove_var("HOMEDRIVE");
                std::env::remove_var("HOMEPATH");
            }
        }
    }

    test_fn();

    if let Some(home) = old_home {
        std::env::set_var("HOME", home);
    } else {
        std::env::remove_var("HOME");
    }

    if let Some(config_dir) = old_config_dir {
        std::env::set_var("RADIOLEDGER_CONFIG_DIR", config_dir);
    } else {
        std::env::remove_var("RADIOLEDGER_CONFIG_DIR");
    }

    #[cfg(windows)]
    {
        if let Some(userprofile) = old_userprofile {
            std::env::set_var("USERPROFILE", userprofile);
        } else {
            std::env::remove_var("USERPROFILE");
        }

        if let Some(homedrive) = old_homedrive {
            std::env::set_var("HOMEDRIVE", homedrive);
        } else {
            std::env::remove_var("HOMEDRIVE");
        }

        if let Some(homepath) = old_homepath {
            std::env::set_var("HOMEPATH", homepath);
        } else {
            std::env::remove_var("HOMEPATH");
        }
    }

    let _ = std::fs::remove_dir_all(&temp_home);
}
