use app::run;

#[test]
fn runs_the_engine() {
    assert!(run() > 0);
}
