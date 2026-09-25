use crate::{Repo, TABLE};

pub struct PgRepo {
    table: String,
}

impl PgRepo {
    pub fn new() -> Self {
        PgRepo { table: TABLE.to_string() }
    }

    pub fn table() -> &'static str {
        TABLE
    }

    fn insert(&self, id: u32) -> bool {
        id > 0
    }
}

impl Repo for PgRepo {
    fn save(&self, id: u32) -> bool {
        self.insert(id)
    }
}
