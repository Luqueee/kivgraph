//! Function values: the forms that name a function without calling it.
//!
//! Every item here is exercised by the loader tests. The negatives matter as
//! much as the positives: a constant standing where a value goes must not be
//! read as a function travelling anywhere.

pub const LIMIT: i32 = 3;

/// double is the function every form below names.
pub fn double(value: i32) -> i32 {
    value * 2
}

/// apply is the higher order function that receives one.
pub fn apply(operation: fn(i32) -> i32, value: i32) -> i32 {
    operation(value)
}

/// takes_limit receives a plain number, never a function.
pub fn takes_limit(bound: i32) -> i32 {
    bound
}

/// passes_double hands `double` over without calling it: the argument is
/// PASSES_AS_CALLBACK while `apply` in the same expression is CALLS_DIRECT.
pub fn passes_double(value: i32) -> i32 {
    apply(double, value)
}

/// binds_double names the function instead of calling it: ASSIGNS_FUNCTION.
pub fn binds_double(value: i32) -> i32 {
    let operation = double;
    operation(value)
}

/// picks_double returns it as the tail expression of the body, which is how
/// Rust returns without a keyword: RETURNS_FUNCTION.
pub fn picks_double() -> fn(i32) -> i32 {
    double
}

/// returns_explicitly writes the keyword. The relation is the same one.
pub fn returns_explicitly() -> fn(i32) -> i32 {
    return double;
}

/// passes_limit is the negative case of an argument: a constant is not a
/// callback however the grammar shapes the call.
pub fn passes_limit() -> i32 {
    takes_limit(LIMIT)
}

/// binds_limit is the negative case of a binding.
pub fn binds_limit() -> i32 {
    let bound = LIMIT;
    bound
}
