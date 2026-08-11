use support::{double, Value};

/// run calls into the provider crate and builds one of its types.
pub fn run(seed: i32) -> Value {
    Value {
        inner: double(seed),
    }
}
