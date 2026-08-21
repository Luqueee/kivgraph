class Vehicle {
  String drive() => 'ready';
}

class ElectricVehicle extends Vehicle with Chargeable implements Transport {
  @override
  String drive() => 'electric';
}

mixin Chargeable {
  int charge() => 100;
}

abstract class Transport {
  String drive();
}
