pub fn clean(s: &str) -> String {
    s.trim().to_string()
}

macro_rules! audit {
    ($e:expr) => {
        let _ = $e;
    };
}
