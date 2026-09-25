pub struct Order {
    pub id: u32,
}

impl Order {
    pub fn new(id: u32) -> Self {
        Order { id }
    }
}

pub enum State {
    New,
    Done,
}
