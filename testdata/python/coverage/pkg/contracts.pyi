from typing import Protocol


class Runner(Protocol):
    def run(self) -> str: ...


def make_runner() -> Runner: ...
