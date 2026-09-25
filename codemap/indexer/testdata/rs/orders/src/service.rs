use acme_store::{pg::PgRepo, Repo};
use crate::order::*;
use super::util;

pub struct OrderService {
    repo: Box<dyn Repo>,
}

impl OrderService {
    pub fn new() -> Self {
        OrderService { repo: Box::new(PgRepo::new()) }
    }

    pub fn place(&self, o: &Order) -> bool {
        self.validate(o);
        let t = PgRepo::table();
        let clean = util::clean(t);
        audit!(clean);
        let _s = State::New;
        self.repo.save(o.id)
    }

    fn validate(&self, o: &Order) {
        let _ = o.id;
    }
}

pub fn run() {
    let svc = OrderService::new();
    svc.place(&Order::new(1));
}

#[cfg(test)]
#[path = "service_tests.rs"]
mod more_tests;

#[cfg(test)]
mod unit {
    use super::*;

    #[test]
    fn works() {
        run();
    }
}
