namespace Example.Coverage;

public interface IShape
{
    double Area();
}

public enum ShapeKind
{
    Circle,
    Square
}

public readonly record struct Point(double X, double Y);

public abstract class ShapeBase : IShape
{
    protected ShapeBase(Point origin) => Origin = origin;

    public Point Origin { get; }

    public abstract ShapeKind Kind();

    public abstract double Area();

    // The accented literal puts a non-ASCII character before a symbol on the
    // same line, which is the only way a wrong position encoding shows up.
    public override string ToString() => $"olá {Kind()}@{Origin}";
}

public sealed class Circle : ShapeBase
{
    private readonly double radius;

    public Circle(Point origin, double radius) : base(origin) => this.radius = radius;

    public override ShapeKind Kind() => ShapeKind.Circle;

    public override double Area() => Math.PI * radius * radius;
}
