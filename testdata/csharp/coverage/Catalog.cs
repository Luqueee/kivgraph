namespace Example.Coverage;

/// <summary>
/// Cross-file references, overloads, a static field, a generic method and a
/// lambda whose body calls a method: the call has no declaration of its own,
/// so it is attributed to the method that holds the lambda.
/// </summary>
public sealed class Catalog
{
    private const string Label = "catalog";

    private readonly List<IShape> entries = new();

    public Catalog Add(IShape shape)
    {
        entries.Add(shape);
        return this;
    }

    /// An overload: it must not share a stable key with the one above.
    public Catalog Add(Point origin, double radius) => Add(new Circle(origin, radius));

    public double Total() => entries.Sum(shape => shape.Area());

    public IReadOnlyList<string> Describe(params string[] prefixes)
    {
        var described = new List<string>();
        foreach (var prefix in prefixes)
        {
            entries.ForEach(shape => described.Add(prefix + Label + shape.Area()));
        }
        return described;
    }

    public static TItem First<TItem>(IReadOnlyList<TItem> values) => values[0];
}
