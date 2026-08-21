library language_features;

enum VehicleKind { car, bike }

sealed class Result<T> {
  const Result();
}

final class Success<T> extends Result<T> {
  final T value;

  const Success(this.value);
}

typedef Mapper<T, R> = R Function(T value);

extension type UserId(int value) {
  String asText() => value.toString();
}

({String name, int wheels}) describe(VehicleKind kind) {
  return switch (kind) {
    VehicleKind.car => (name: 'car', wheels: 4),
    VehicleKind.bike => (name: 'bike', wheels: 2),
  };
}
