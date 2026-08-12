use app::run;

fn main() {
    let seed = run();
    std::process::exit(seed as i32);
}
