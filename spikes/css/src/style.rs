//! Modelo de estilo del spike (`SpikeStyle`).
//!
//! Es a propósito más ancho que `browser_lite::css::Style`: añade tipografía
//! (familia, peso, cursiva, interlineado, alineación, tracking) para que las
//! diferencias entre proveedores de CSS sean visibles en el render. Ambos
//! proveedores se mapean a este modelo; ninguna diferencia de render debe venir
//! del pipeline (S2).

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Display {
    Block,
    Inline,
    None,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum TextAlign {
    Left,
    Center,
    Right,
    Justify,
}

pub type Rgba = (f64, f64, f64, f64);

#[derive(Debug, Clone, PartialEq)]
pub struct SpikeStyle {
    pub display: Display,
    pub width: Option<f64>,
    pub height: Option<f64>,
    pub margin: [f64; 4],  // top, right, bottom, left (px)
    pub padding: [f64; 4], // top, right, bottom, left (px)
    pub color: Rgba,
    pub background: Option<Rgba>,
    pub font_family: String,
    pub font_size: f64,
    pub font_weight: u32,
    pub italic: bool,
    pub line_height: f64,    // multiplicador
    pub text_align: TextAlign,
    pub letter_spacing: f64, // px
}

impl Default for SpikeStyle {
    fn default() -> Self {
        SpikeStyle {
            display: Display::Inline,
            width: None,
            height: None,
            margin: [0.0; 4],
            padding: [0.0; 4],
            color: (0.0, 0.0, 0.0, 1.0),
            background: None,
            font_family: "Sans".to_string(),
            font_size: 16.0,
            font_weight: 400,
            italic: false,
            line_height: 1.2,
            text_align: TextAlign::Left,
            letter_spacing: 0.0,
        }
    }
}

impl SpikeStyle {
    /// Comparación de colores con tolerancia: los colores llegan como f64 desde
    /// dos caminos distintos (u8/255 y coma fija) y el bit final no es el objetivo.
    pub fn color_eq(a: Option<Rgba>, b: Option<Rgba>) -> bool {
        match (a, b) {
            (None, None) => true,
            (Some(a), Some(b)) => {
                [a.0, a.1, a.2, a.3]
                    .iter()
                    .zip([b.0, b.1, b.2, b.3].iter())
                    .all(|(x, y)| (x - y).abs() < 0.005)
            }
            _ => false,
        }
    }

    pub fn near(a: f64, b: f64) -> bool {
        (a - b).abs() < 0.01
    }
}
