from __future__ import annotations

from collections.abc import Callable
from typing import overload

from .contracts import Runner, make_runner
from .models import Box, Car, Named, Vehicle


@overload
def convert(value: int) -> str: ...


@overload
def convert(value: str) -> int: ...


def convert(value: int | str) -> int | str:
    return str(value) if isinstance(value, int) else len(value)


def run_callback(callback: Callable[[Vehicle], str], vehicle: Vehicle) -> str:
    return callback(vehicle)


def build() -> str:
    vehicle = Car()
    box: Box[Vehicle] = Box(vehicle)
    named: Named = vehicle
    runner: Runner = make_runner()
    return convert(box.get().drive()) + runner.run() + run_callback(lambda item: item.name, named)
