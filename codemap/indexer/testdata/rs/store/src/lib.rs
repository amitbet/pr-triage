pub mod pg;

pub const TABLE: &str = "orders";

/// Saves orders.
pub trait Repo {
    fn save(&self, id: u32) -> bool;
    fn count(&self) -> usize {
        0
    }
}
