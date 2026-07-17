use std::path::PathBuf;

use image::{DynamicImage, Rgba, RgbaImage};

fn main() {
    generate_tray_icons().expect("failed to generate tray icons");
    tauri_build::build();
}

fn generate_tray_icons() -> Result<(), Box<dyn std::error::Error>> {
    let manifest_dir = PathBuf::from(std::env::var("CARGO_MANIFEST_DIR")?);
    let base_icon_path = manifest_dir.join("icons/32x32.png");
    let out_dir = manifest_dir.join("icons/tray");

    std::fs::create_dir_all(&out_dir)?;

    println!("cargo:rerun-if-changed={}", base_icon_path.display());

    let base = image::open(&base_icon_path)?.resize_exact(32, 32, image::imageops::FilterType::Lanczos3);

    let idle = base.to_rgba8();
    idle.save(out_dir.join("tray-idle.png"))?;

    let mut pending = base.to_rgba8();
    draw_status_dot(&mut pending, Rgba([245, 158, 11, 255]));
    pending.save(out_dir.join("tray-pending.png"))?;

    let mut error = base.to_rgba8();
    draw_status_dot(&mut error, Rgba([220, 38, 38, 255]));
    error.save(out_dir.join("tray-error.png"))?;

    let mut disconnected = desaturate(base);
    draw_status_dot(&mut disconnected, Rgba([156, 163, 175, 255]));
    disconnected.save(out_dir.join("tray-disconnected.png"))?;

    Ok(())
}

fn desaturate(base: DynamicImage) -> RgbaImage {
    let mut rgba = base.to_rgba8();

    for p in rgba.pixels_mut() {
        let [r, g, b, a] = p.0;
        let lum = (0.299 * f32::from(r) + 0.587 * f32::from(g) + 0.114 * f32::from(b)) as u8;
        let toned = ((f32::from(lum) * 0.8) as u8).max(40);
        *p = Rgba([toned, toned, toned, a]);
    }

    rgba
}

fn draw_status_dot(img: &mut RgbaImage, color: Rgba<u8>) {
    let cx = 24i32;
    let cy = 24i32;
    let r_outer = 5i32;
    let r_inner = 4i32;

    for y in (cy - r_outer)..=(cy + r_outer) {
        for x in (cx - r_outer)..=(cx + r_outer) {
            if x < 0 || y < 0 || x >= 32 || y >= 32 {
                continue;
            }

            let dx = x - cx;
            let dy = y - cy;
            let dist2 = dx * dx + dy * dy;

            if dist2 <= r_outer * r_outer {
                let px = img.get_pixel_mut(x as u32, y as u32);
                if dist2 >= r_inner * r_inner {
                    *px = Rgba([25, 25, 25, 220]);
                } else {
                    *px = color;
                }
            }
        }
    }
}
