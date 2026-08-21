from __future__ import annotations

from typing import Generic, Protocol, TypeVar

T = TypeVar("T")


class Named(Protocol):
    name: str


class Box(Generic[T]):
    def __init__(self, value: T) -> None:
        self.value = value

    def get(self) -> T:
        return self.value


class Vehicle:
    name = "vehicle"

    def drive(self) -> str:
        return self.name


class Car(Vehicle):
    def drive(self) -> str:
        return "car"
