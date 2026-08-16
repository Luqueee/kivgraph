package layout

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"sort"

	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
)

// Coord is a signed fixed-point layout coordinate. Layout calculations never
// use floating-point arithmetic, so identical snapshot/configuration pairs
// produce byte-identical positions on every supported architecture.
type Coord int64

const maxCoord = Coord(1<<63 - 1)

var (
	ErrNilSnapshot        = errors.New("layout snapshot is nil")
	ErrNilLayout          = errors.New("layout is nil")
	ErrInvalidConfig      = errors.New("layout configuration is invalid")
	ErrInvalidContainment = errors.New("snapshot containment is invalid")
	ErrLayoutOverflow     = errors.New("layout coordinate overflow")
	ErrLayoutTooLarge     = errors.New("layout is too large")
	ErrInvalidViewport    = errors.New("layout viewport is invalid")
	ErrInvalidLOD         = errors.New("layout level of detail is invalid")
	ErrViewportNodeLimit  = errors.New("layout viewport node limit is invalid")
	ErrViewportTooLarge   = errors.New("layout viewport exceeds cell limit")
)

// NodeKind identifies the hierarchy level represented by a Node.
type NodeKind uint8

const (
	NodeNone NodeKind = iota
	NodeRepository
	NodePackage
	NodeFile
	NodeSymbol
)

// LOD is the deepest hierarchy level returned by a viewport query. A query at
// LODPackages may return repositories and packages, but never files or symbols.
type LOD uint8

const (
	LODRepositories LOD = iota
	LODPackages
	LODFiles
	LODSymbols
)

// DenseID is scoped by NodeRef.Kind and therefore cannot be mistaken for a
// repository, package, file, or symbol ID outside that discriminant.
type DenseID uint32

// NodeRef identifies a dense snapshot node together with its table.
type NodeRef struct {
	Kind NodeKind
	ID   DenseID
}

// Node is an immutable positioned hierarchy node. ID is interpreted according
// to Kind and is the dense ID in the source HotSnapshot table.
type Node struct {
	Kind   NodeKind
	Level  LOD
	ID     DenseID
	Parent NodeRef
	Bounds Rect
}

// Rect is a half-open axis-aligned rectangle: [MinX, MaxX) x [MinY, MaxY).
type Rect struct {
	MinX Coord
	MinY Coord
	MaxX Coord
	MaxY Coord
}

// Valid reports whether the rectangle has positive area.
func (rect Rect) Valid() bool {
	return rect.MinX < rect.MaxX && rect.MinY < rect.MaxY
}

// Intersects reports whether two half-open rectangles share area.
func (rect Rect) Intersects(other Rect) bool {
	return rect.MinX < other.MaxX && other.MinX < rect.MaxX &&
		rect.MinY < other.MaxY && other.MinY < rect.MaxY
}

// Config controls the deterministic hierarchy packer and spatial index.
type Config struct {
	SymbolWidth  Coord
	SymbolHeight Coord
	SymbolGap    Coord

	FilePadding       Coord
	FileGap           Coord
	PackagePadding    Coord
	PackageGap        Coord
	RepositoryPadding Coord
	RepositoryGap     Coord

	// Columns fixes the width of every child grid. Zero balances each
	// container instead: ceil(sqrt(children)), so a workspace does not become
	// a column dozens of screens tall that no viewport can read.
	Columns  int
	CellSize Coord

	// MaxIndexedCellsPerNode prevents large containers from being copied into
	// thousands of grid buckets. Such nodes are kept in an ordered overflow
	// list and checked directly during a query.
	MaxIndexedCellsPerNode int
	MaxQueryCells          int
	MaxQueryNodes          int
}

// DefaultConfig returns the reproducible layout configuration used by the
// viewer unless a caller supplies an explicit configuration.
func DefaultConfig() Config {
	return Config{
		SymbolWidth:            160,
		SymbolHeight:           32,
		SymbolGap:              16,
		FilePadding:            24,
		FileGap:                32,
		PackagePadding:         32,
		PackageGap:             48,
		RepositoryPadding:      48,
		RepositoryGap:          72,
		Columns:                0,
		CellSize:               256,
		MaxIndexedCellsPerNode: 128,
		MaxQueryCells:          1_000_000,
		MaxQueryNodes:          10_000,
	}
}

// Validate checks all configuration values before layout allocation begins.
func (config Config) Validate() error {
	positive := []struct {
		name  string
		value Coord
	}{
		{"symbol width", config.SymbolWidth},
		{"symbol height", config.SymbolHeight},
		{"cell size", config.CellSize},
	}
	for _, item := range positive {
		if item.value <= 0 {
			return fmt.Errorf("%w: %s must be positive", ErrInvalidConfig, item.name)
		}
	}
	nonNegative := []struct {
		name  string
		value Coord
	}{
		{"symbol gap", config.SymbolGap},
		{"file padding", config.FilePadding},
		{"file gap", config.FileGap},
		{"package padding", config.PackagePadding},
		{"package gap", config.PackageGap},
		{"repository padding", config.RepositoryPadding},
		{"repository gap", config.RepositoryGap},
	}
	for _, item := range nonNegative {
		if item.value < 0 {
			return fmt.Errorf("%w: %s must not be negative", ErrInvalidConfig, item.name)
		}
	}
	if config.Columns < 0 || config.Columns > 1<<20 {
		return fmt.Errorf("%w: columns must be 0 (balanced) or between 1 and %d", ErrInvalidConfig, 1<<20)
	}
	if config.MaxIndexedCellsPerNode <= 0 || config.MaxQueryCells <= 0 || config.MaxQueryNodes <= 0 {
		return fmt.Errorf("%w: grid and query limits must be positive", ErrInvalidConfig)
	}
	if config.MaxIndexedCellsPerNode > 1<<30 || config.MaxQueryCells > 1<<30 || config.MaxQueryNodes > 1<<30 {
		return fmt.Errorf("%w: grid and query limits are too large", ErrInvalidConfig)
	}
	for _, value := range []Coord{
		config.SymbolWidth, config.SymbolHeight, config.SymbolGap,
		config.FilePadding, config.FileGap, config.PackagePadding, config.PackageGap,
		config.RepositoryPadding, config.RepositoryGap, config.CellSize,
	} {
		if _, err := checkedMultiply(value, 2); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidConfig, err)
		}
	}
	return nil
}

// ViewportQuery limits a spatial query by bounds, detail level, and result
// count. Bounds use the same half-open convention as Rect.
type ViewportQuery struct {
	Bounds   Rect
	MaxLevel LOD
	MaxNodes int
}

// ViewportResult is ordered by the immutable layout node order. Truncated is
// true only when at least one additional matching node was omitted by MaxNodes.
type ViewportResult struct {
	Nodes     []Node
	Truncated bool
}

type nodeSize struct {
	width  Coord
	height Coord
}

type gridMetrics struct {
	size         nodeSize
	columnWidths []Coord
	rowHeights   []Coord
}

type cellKey struct {
	x Coord
	y Coord
}

type spatialGrid struct {
	cellSize   Coord
	cells      map[cellKey][]int
	overflow   []int
	entryCount int
}

// Layout is an immutable hierarchy layout and its read-only spatial index.
type Layout struct {
	snapshotID uint64
	config     Config
	bounds     Rect
	nodes      []Node
	grid       spatialGrid
}

// SnapshotID returns the source HotSnapshot identifier.
func (layout *Layout) SnapshotID() uint64 {
	if layout == nil {
		return 0
	}
	return layout.snapshotID
}

// Config returns the value configuration used to build the layout.
func (layout *Layout) Config() Config {
	if layout == nil {
		return Config{}
	}
	return layout.config
}

// Bounds returns the root layout bounds. An empty snapshot has zero bounds.
func (layout *Layout) Bounds() Rect {
	if layout == nil {
		return Rect{}
	}
	return layout.bounds
}

// Nodes returns a copy of all nodes in deterministic repository/package/file/
// symbol order.
func (layout *Layout) Nodes() []Node {
	if layout == nil {
		return nil
	}
	return append([]Node(nil), layout.nodes...)
}

// GridStats describes the spatial index without exposing its mutable maps.
type GridStats struct {
	Cells          int
	IndexedEntries int
	OverflowNodes  int
}

// GridStats returns deterministic aggregate index statistics.
func (layout *Layout) GridStats() GridStats {
	if layout == nil {
		return GridStats{}
	}
	return GridStats{
		Cells:          len(layout.grid.cells),
		IndexedEntries: layout.grid.entryCount,
		OverflowNodes:  len(layout.grid.overflow),
	}
}

// Build lays out repository -> package -> file -> symbol with deterministic
// row-major packing. It never performs force simulation or graph traversal.
func Build(ctx context.Context, snapshot *hotsnapshot.GraphSnapshot, config Config) (*Layout, error) {
	if snapshot == nil {
		return nil, ErrNilSnapshot
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := contextError(ctx); err != nil {
		return nil, err
	}

	metadata := snapshot.Metadata()
	repositoryCount, err := denseCount(metadata.Counts.Repositories)
	if err != nil {
		return nil, fmt.Errorf("repositories: %w", err)
	}
	packageCount, err := denseCount(metadata.Counts.Packages)
	if err != nil {
		return nil, fmt.Errorf("packages: %w", err)
	}
	fileCount, err := denseCount(metadata.Counts.Files)
	if err != nil {
		return nil, fmt.Errorf("files: %w", err)
	}
	symbolCount, err := denseCount(metadata.Counts.Symbols)
	if err != nil {
		return nil, fmt.Errorf("symbols: %w", err)
	}

	packagesByRepository := make([][]hotsnapshot.PackageID, repositoryCount)
	packageRepositories := make([]hotsnapshot.RepositoryID, packageCount)
	if err := snapshot.VisitPackages(ctx, 0, hotsnapshot.PackageID(packageCount), func(id hotsnapshot.PackageID, record hotsnapshot.PackageRecord) error {
		if uint64(record.Repository) >= uint64(repositoryCount) {
			return fmt.Errorf("%w: package %d has repository %d", ErrInvalidContainment, id, record.Repository)
		}
		packageRepositories[id] = record.Repository
		packagesByRepository[record.Repository] = append(packagesByRepository[record.Repository], id)
		return nil
	}); err != nil {
		return nil, err
	}

	filesByPackage := make([][]hotsnapshot.FileID, packageCount)
	filePackages := make([]hotsnapshot.PackageID, fileCount)
	if err := snapshot.VisitFiles(ctx, 0, hotsnapshot.FileID(fileCount), func(id hotsnapshot.FileID, record hotsnapshot.FileRecord) error {
		if uint64(record.Package) >= uint64(packageCount) {
			return fmt.Errorf("%w: file %d has package %d", ErrInvalidContainment, id, record.Package)
		}
		if record.Repository != packageRepositories[record.Package] {
			return fmt.Errorf("%w: file %d repository %d disagrees with package %d repository %d", ErrInvalidContainment, id, record.Repository, record.Package, packageRepositories[record.Package])
		}
		filePackages[id] = record.Package
		filesByPackage[record.Package] = append(filesByPackage[record.Package], id)
		return nil
	}); err != nil {
		return nil, err
	}

	symbolsByFile := make([][]hotsnapshot.SymbolID, fileCount)
	symbolFiles := make([]hotsnapshot.FileID, symbolCount)
	if err := snapshot.VisitSymbols(ctx, 0, hotsnapshot.SymbolID(symbolCount), func(id hotsnapshot.SymbolID, record hotsnapshot.SymbolRecord) error {
		if uint64(record.File) >= uint64(fileCount) {
			return fmt.Errorf("%w: symbol %d has file %d", ErrInvalidContainment, id, record.File)
		}
		symbolFiles[id] = record.File
		symbolsByFile[record.File] = append(symbolsByFile[record.File], id)
		return nil
	}); err != nil {
		return nil, err
	}

	symbolSizes := make([]nodeSize, symbolCount)
	for index := range symbolSizes {
		if err := contextError(ctx); err != nil {
			return nil, err
		}
		symbolSizes[index] = nodeSize{width: config.SymbolWidth, height: config.SymbolHeight}
	}

	fileSizes := make([]nodeSize, fileCount)
	minimumFile, err := minimumContainer(config.FilePadding, nodeSize{width: config.SymbolWidth, height: config.SymbolHeight}, config.SymbolWidth, config.SymbolHeight)
	if err != nil {
		return nil, err
	}
	for index, children := range symbolsByFile {
		if err := contextError(ctx); err != nil {
			return nil, err
		}
		metrics, err := metricsForChildren(children, symbolSizes, gridColumns(config, children, symbolSizes), config.SymbolGap, config.FilePadding, minimumFile)
		if err != nil {
			return nil, fmt.Errorf("file %d: %w", index, err)
		}
		fileSizes[index] = metrics.size
	}

	minimumPackage, err := minimumContainer(config.PackagePadding, minimumFile, minimumFile.width, minimumFile.height)
	if err != nil {
		return nil, err
	}
	packageSizes := make([]nodeSize, packageCount)
	for index, children := range filesByPackage {
		if err := contextError(ctx); err != nil {
			return nil, err
		}
		metrics, err := metricsForChildren(children, fileSizes, gridColumns(config, children, fileSizes), config.FileGap, config.PackagePadding, minimumPackage)
		if err != nil {
			return nil, fmt.Errorf("package %d: %w", index, err)
		}
		packageSizes[index] = metrics.size
	}

	minimumRepository, err := minimumContainer(config.RepositoryPadding, minimumPackage, minimumPackage.width, minimumPackage.height)
	if err != nil {
		return nil, err
	}
	repositorySizes := make([]nodeSize, repositoryCount)
	repositoryIDs := make([]hotsnapshot.RepositoryID, repositoryCount)
	for index := range repositoryIDs {
		repositoryIDs[index] = hotsnapshot.RepositoryID(index)
	}
	for index, children := range packagesByRepository {
		if err := contextError(ctx); err != nil {
			return nil, err
		}
		metrics, err := metricsForChildren(children, packageSizes, gridColumns(config, children, packageSizes), config.PackageGap, config.RepositoryPadding, minimumRepository)
		if err != nil {
			return nil, fmt.Errorf("repository %d: %w", index, err)
		}
		repositorySizes[index] = metrics.size
	}

	rootMetrics, err := metricsForChildren(repositoryIDs, repositorySizes, gridColumns(config, repositoryIDs, repositorySizes), config.RepositoryGap, 0, nodeSize{})
	if err != nil {
		return nil, fmt.Errorf("root: %w", err)
	}
	repositoryBounds := make([]Rect, repositoryCount)
	packageBounds := make([]Rect, packageCount)
	fileBounds := make([]Rect, fileCount)
	symbolBounds := make([]Rect, symbolCount)

	if err := placeChildren(repositoryIDs, repositorySizes, gridColumns(config, repositoryIDs, repositorySizes), config.RepositoryGap, 0, 0, 0, func(id hotsnapshot.RepositoryID, bounds Rect) error {
		repositoryBounds[id] = bounds
		return nil
	}); err != nil {
		return nil, fmt.Errorf("root placement: %w", err)
	}
	for repositoryID, children := range packagesByRepository {
		if err := contextError(ctx); err != nil {
			return nil, err
		}
		originX, err := checkedAdd(repositoryBounds[repositoryID].MinX, config.RepositoryPadding)
		if err != nil {
			return nil, err
		}
		originY, err := checkedAdd(repositoryBounds[repositoryID].MinY, config.RepositoryPadding)
		if err != nil {
			return nil, err
		}
		if err := placeChildren(children, packageSizes, gridColumns(config, children, packageSizes), config.PackageGap, config.RepositoryPadding, originX, originY, func(id hotsnapshot.PackageID, bounds Rect) error {
			packageBounds[id] = bounds
			return nil
		}); err != nil {
			return nil, fmt.Errorf("repository %d placement: %w", repositoryID, err)
		}
	}
	for packageID, children := range filesByPackage {
		if err := contextError(ctx); err != nil {
			return nil, err
		}
		originX, err := checkedAdd(packageBounds[packageID].MinX, config.PackagePadding)
		if err != nil {
			return nil, err
		}
		originY, err := checkedAdd(packageBounds[packageID].MinY, config.PackagePadding)
		if err != nil {
			return nil, err
		}
		if err := placeChildren(children, fileSizes, gridColumns(config, children, fileSizes), config.FileGap, config.PackagePadding, originX, originY, func(id hotsnapshot.FileID, bounds Rect) error {
			fileBounds[id] = bounds
			return nil
		}); err != nil {
			return nil, fmt.Errorf("package %d placement: %w", packageID, err)
		}
	}
	for fileID, children := range symbolsByFile {
		if err := contextError(ctx); err != nil {
			return nil, err
		}
		originX, err := checkedAdd(fileBounds[fileID].MinX, config.FilePadding)
		if err != nil {
			return nil, err
		}
		originY, err := checkedAdd(fileBounds[fileID].MinY, config.FilePadding)
		if err != nil {
			return nil, err
		}
		if err := placeChildren(children, symbolSizes, gridColumns(config, children, symbolSizes), config.SymbolGap, config.FilePadding, originX, originY, func(id hotsnapshot.SymbolID, bounds Rect) error {
			symbolBounds[id] = bounds
			return nil
		}); err != nil {
			return nil, fmt.Errorf("file %d placement: %w", fileID, err)
		}
	}

	totalNodes, err := totalNodeCount(repositoryCount, packageCount, fileCount, symbolCount)
	if err != nil {
		return nil, err
	}
	nodes := make([]Node, 0, totalNodes)
	for id, bounds := range repositoryBounds {
		nodes = append(nodes, Node{Kind: NodeRepository, Level: LODRepositories, ID: DenseID(id), Bounds: bounds})
	}
	for id, bounds := range packageBounds {
		nodes = append(nodes, Node{Kind: NodePackage, Level: LODPackages, ID: DenseID(id), Parent: NodeRef{Kind: NodeRepository, ID: DenseID(packageRepositories[id])}, Bounds: bounds})
	}
	for id, bounds := range fileBounds {
		nodes = append(nodes, Node{Kind: NodeFile, Level: LODFiles, ID: DenseID(id), Parent: NodeRef{Kind: NodePackage, ID: DenseID(filePackages[id])}, Bounds: bounds})
	}
	for id, bounds := range symbolBounds {
		nodes = append(nodes, Node{Kind: NodeSymbol, Level: LODSymbols, ID: DenseID(id), Parent: NodeRef{Kind: NodeFile, ID: DenseID(symbolFiles[id])}, Bounds: bounds})
	}
	for index, node := range nodes {
		if !node.Bounds.Valid() {
			return nil, fmt.Errorf("%w: node %d has empty bounds", ErrLayoutOverflow, index)
		}
	}
	grid, err := buildGrid(nodes, config)
	if err != nil {
		return nil, err
	}
	return &Layout{
		snapshotID: metadata.ID,
		config:     config,
		bounds:     Rect{MaxX: rootMetrics.size.width, MaxY: rootMetrics.size.height},
		nodes:      nodes,
		grid:       grid,
	}, nil
}

func denseCount(value uint64) (int, error) {
	if value >= uint64(^uint32(0)) || value > uint64(maxIntValue()) {
		return 0, ErrLayoutTooLarge
	}
	return int(value), nil
}

func maxIntValue() int {
	return int(^uint(0) >> 1)
}

func totalNodeCount(counts ...int) (int, error) {
	total := 0
	for _, count := range counts {
		if count < 0 || total > maxIntValue()-count {
			return 0, ErrLayoutTooLarge
		}
		total += count
	}
	return total, nil
}

func minimumContainer(padding Coord, fallback nodeSize, width, height Coord) (nodeSize, error) {
	doubledPadding, err := checkedMultiply(padding, 2)
	if err != nil {
		return nodeSize{}, err
	}
	minimumWidth, err := checkedAdd(doubledPadding, width)
	if err != nil {
		return nodeSize{}, err
	}
	minimumHeight, err := checkedAdd(doubledPadding, height)
	if err != nil {
		return nodeSize{}, err
	}
	return nodeSize{width: maxCoordValue(minimumWidth, fallback.width), height: maxCoordValue(minimumHeight, fallback.height)}, nil
}

// gridColumns is the width of one container's child grid. A fixed Columns is
// honoured as configured; zero balances the grid so the container comes out
// roughly square.
//
// Balancing by child count alone is not enough: a symbol box is five times
// wider than it is tall, so a square count grid produces a very wide strip.
// The width is derived from the child aspect ratio instead.
func gridColumns[ID ~uint32](config Config, ids []ID, sizes []nodeSize) int {
	if config.Columns > 0 {
		return config.Columns
	}
	if len(ids) <= 1 {
		return 1
	}
	var width, height float64
	for _, id := range ids {
		if uint64(id) >= uint64(len(sizes)) {
			continue
		}
		width += float64(sizes[id].width)
		height += float64(sizes[id].height)
	}
	if width <= 0 || height <= 0 {
		return int(math.Ceil(math.Sqrt(float64(len(ids)))))
	}
	count := float64(len(ids))
	averageWidth := width / count
	averageHeight := height / count
	columns := int(math.Round(math.Sqrt(count * averageHeight / averageWidth)))
	if columns < 1 {
		return 1
	}
	if columns > len(ids) {
		return len(ids)
	}
	return columns
}

func metricsForChildren[ID ~uint32](ids []ID, sizes []nodeSize, columns int, gap, padding Coord, minimum nodeSize) (gridMetrics, error) {
	metrics := gridMetrics{}
	if len(ids) == 0 {
		doubledPadding, err := checkedMultiply(padding, 2)
		if err != nil {
			return metrics, err
		}
		metrics.size = nodeSize{
			width:  maxCoordValue(doubledPadding, minimum.width),
			height: maxCoordValue(doubledPadding, minimum.height),
		}
		return metrics, nil
	}
	if columns <= 0 {
		return metrics, ErrInvalidConfig
	}
	if columns > len(ids) {
		columns = len(ids)
	}
	rows := (len(ids)-1)/columns + 1
	metrics.columnWidths = make([]Coord, columns)
	metrics.rowHeights = make([]Coord, rows)
	for index, id := range ids {
		if uint64(id) >= uint64(len(sizes)) {
			return gridMetrics{}, fmt.Errorf("%w: child ID %d is outside dimension table", ErrInvalidContainment, id)
		}
		size := sizes[id]
		if size.width <= 0 || size.height <= 0 {
			return gridMetrics{}, fmt.Errorf("%w: child ID %d has non-positive dimensions", ErrLayoutOverflow, id)
		}
		column := index % columns
		row := index / columns
		metrics.columnWidths[column] = maxCoordValue(metrics.columnWidths[column], size.width)
		metrics.rowHeights[row] = maxCoordValue(metrics.rowHeights[row], size.height)
	}
	contentWidth, err := sumWithGaps(metrics.columnWidths, gap)
	if err != nil {
		return gridMetrics{}, err
	}
	contentHeight, err := sumWithGaps(metrics.rowHeights, gap)
	if err != nil {
		return gridMetrics{}, err
	}
	doubledPadding, err := checkedMultiply(padding, 2)
	if err != nil {
		return gridMetrics{}, err
	}
	width, err := checkedAdd(doubledPadding, contentWidth)
	if err != nil {
		return gridMetrics{}, err
	}
	height, err := checkedAdd(doubledPadding, contentHeight)
	if err != nil {
		return gridMetrics{}, err
	}
	metrics.size = nodeSize{width: maxCoordValue(width, minimum.width), height: maxCoordValue(height, minimum.height)}
	return metrics, nil
}

func sumWithGaps(values []Coord, gap Coord) (Coord, error) {
	var total Coord
	for _, value := range values {
		var err error
		total, err = checkedAdd(total, value)
		if err != nil {
			return 0, err
		}
	}
	if len(values) > 1 {
		gaps, err := checkedMultiply(gap, Coord(len(values)-1))
		if err != nil {
			return 0, err
		}
		total, err = checkedAdd(total, gaps)
		if err != nil {
			return 0, err
		}
	}
	return total, nil
}

func placeChildren[ID ~uint32](ids []ID, sizes []nodeSize, columns int, gap, padding, originX, originY Coord, place func(ID, Rect) error) error {
	if len(ids) == 0 {
		return nil
	}
	if place == nil {
		return errors.New("layout child placer is nil")
	}
	metrics, err := metricsForChildren(ids, sizes, columns, gap, padding, nodeSize{})
	if err != nil {
		return err
	}
	columns = len(metrics.columnWidths)
	for index, id := range ids {
		column := index % columns
		row := index / columns
		x := originX
		for previous := 0; previous < column; previous++ {
			x, err = checkedAdd(x, metrics.columnWidths[previous])
			if err != nil {
				return err
			}
			x, err = checkedAdd(x, gap)
			if err != nil {
				return err
			}
		}
		y := originY
		for previous := 0; previous < row; previous++ {
			y, err = checkedAdd(y, metrics.rowHeights[previous])
			if err != nil {
				return err
			}
			y, err = checkedAdd(y, gap)
			if err != nil {
				return err
			}
		}
		size := sizes[id]
		maxX, err := checkedAdd(x, size.width)
		if err != nil {
			return err
		}
		maxY, err := checkedAdd(y, size.height)
		if err != nil {
			return err
		}
		if err := place(id, Rect{MinX: x, MinY: y, MaxX: maxX, MaxY: maxY}); err != nil {
			return err
		}
	}
	return nil
}

func checkedAdd(left, right Coord) (Coord, error) {
	if left < 0 || right < 0 || left > maxCoord-right {
		return 0, ErrLayoutOverflow
	}
	return left + right, nil
}

func checkedMultiply(left, right Coord) (Coord, error) {
	if left < 0 || right < 0 || (right != 0 && left > maxCoord/right) {
		return 0, ErrLayoutOverflow
	}
	return left * right, nil
}

func maxCoordValue(left, right Coord) Coord {
	if left > right {
		return left
	}
	return right
}

func contextError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

func buildGrid(nodes []Node, config Config) (spatialGrid, error) {
	grid := spatialGrid{cellSize: config.CellSize, cells: make(map[cellKey][]int)}
	for index, node := range nodes {
		minX, maxX, minY, maxY, spanX, spanY, err := cellRange(node.Bounds, config.CellSize)
		if err != nil {
			return spatialGrid{}, err
		}
		if spanX > Coord(config.MaxIndexedCellsPerNode) || spanY > Coord(config.MaxIndexedCellsPerNode)/spanX {
			grid.overflow = append(grid.overflow, index)
			continue
		}
		for y := minY; ; y++ {
			for x := minX; ; x++ {
				grid.cells[cellKey{x: x, y: y}] = append(grid.cells[cellKey{x: x, y: y}], index)
				grid.entryCount++
				if x == maxX {
					break
				}
			}
			if y == maxY {
				break
			}
		}
	}
	return grid, nil
}

func cellRange(rect Rect, cellSize Coord) (minX, maxX, minY, maxY, spanX, spanY Coord, err error) {
	if !rect.Valid() {
		return 0, 0, 0, 0, 0, 0, ErrInvalidViewport
	}
	minX = cellCoordinate(rect.MinX, cellSize)
	maxX = cellCoordinate(rect.MaxX-1, cellSize)
	minY = cellCoordinate(rect.MinY, cellSize)
	maxY = cellCoordinate(rect.MaxY-1, cellSize)
	spanX, err = cellSpan(minX, maxX)
	if err != nil {
		return 0, 0, 0, 0, 0, 0, err
	}
	spanY, err = cellSpan(minY, maxY)
	if err != nil {
		return 0, 0, 0, 0, 0, 0, err
	}
	return minX, maxX, minY, maxY, spanX, spanY, nil
}

func cellCoordinate(value, cellSize Coord) Coord {
	quotient := value / cellSize
	if value < 0 && value%cellSize != 0 {
		quotient--
	}
	return quotient
}

func cellSpan(minimum, maximum Coord) (Coord, error) {
	if maximum < minimum {
		return 0, ErrViewportTooLarge
	}
	difference := uint64(maximum) - uint64(minimum)
	if difference >= uint64(maxCoord) {
		return 0, ErrViewportTooLarge
	}
	return Coord(difference + 1), nil
}

func (layout *Layout) nodeMatches(index int, query ViewportQuery) bool {
	node := layout.nodes[index]
	return node.Level <= query.MaxLevel && node.Bounds.Intersects(query.Bounds)
}

// QueryViewport returns all nodes whose bounds intersect query.Bounds and whose
// detail level is at most query.MaxLevel. It uses the deterministic grid order,
// then restores immutable node order before applying MaxNodes.
func (layout *Layout) QueryViewport(query ViewportQuery) (ViewportResult, error) {
	if layout == nil {
		return ViewportResult{}, ErrNilLayout
	}
	if !query.Bounds.Valid() {
		return ViewportResult{}, ErrInvalidViewport
	}
	if query.MaxLevel > LODSymbols {
		return ViewportResult{}, ErrInvalidLOD
	}
	maxNodes := query.MaxNodes
	if maxNodes == 0 {
		maxNodes = layout.config.MaxQueryNodes
	}
	if maxNodes < 0 || maxNodes > layout.config.MaxQueryNodes {
		return ViewportResult{}, ErrViewportNodeLimit
	}
	minX, maxX, minY, maxY, spanX, spanY, err := cellRange(query.Bounds, layout.grid.cellSize)
	if err != nil {
		return ViewportResult{}, err
	}
	if spanX > Coord(layout.config.MaxQueryCells) || spanY > Coord(layout.config.MaxQueryCells)/spanX {
		return ViewportResult{}, ErrViewportTooLarge
	}

	seen := make([]bool, len(layout.nodes))
	candidates := make([]int, 0)
	addCandidate := func(index int) {
		if index < 0 || index >= len(layout.nodes) || seen[index] {
			return
		}
		seen[index] = true
		candidates = append(candidates, index)
	}
	for _, index := range layout.grid.overflow {
		addCandidate(index)
	}
	for y := minY; ; y++ {
		for x := minX; ; x++ {
			for _, index := range layout.grid.cells[cellKey{x: x, y: y}] {
				addCandidate(index)
			}
			if x == maxX {
				break
			}
		}
		if y == maxY {
			break
		}
	}
	sort.Ints(candidates)
	result := ViewportResult{Nodes: make([]Node, 0, minInt(maxNodes, len(candidates)))}
	for _, index := range candidates {
		if !layout.nodeMatches(index, query) {
			continue
		}
		if len(result.Nodes) < maxNodes {
			result.Nodes = append(result.Nodes, layout.nodes[index])
			continue
		}
		result.Truncated = true
		break
	}
	return result, nil
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

const layoutBinaryVersion uint16 = 1

// MarshalBinary returns a deterministic internal layout encoding used to
// compare two builds. It is not the HTTP viewer wire protocol.
func (layout *Layout) MarshalBinary() ([]byte, error) {
	if layout == nil {
		return nil, ErrNilLayout
	}
	if uint64(len(layout.nodes)) > uint64(^uint32(0)) {
		return nil, ErrLayoutTooLarge
	}
	encoded := make([]byte, 0, 96+len(layout.nodes)*48)
	encoded = append(encoded, 'L', 'G', 'L', 'Y')
	encoded = appendUint16(encoded, layoutBinaryVersion)
	encoded = appendUint16(encoded, 0)
	encoded = appendUint64(encoded, layout.snapshotID)
	encoded = appendRect(encoded, layout.bounds)
	encoded = appendUint32(encoded, uint32(layout.config.Columns))
	encoded = appendUint32(encoded, uint32(layout.config.MaxIndexedCellsPerNode))
	encoded = appendUint32(encoded, uint32(layout.config.MaxQueryCells))
	encoded = appendUint32(encoded, uint32(layout.config.MaxQueryNodes))
	for _, value := range []Coord{
		layout.config.SymbolWidth, layout.config.SymbolHeight, layout.config.SymbolGap,
		layout.config.FilePadding, layout.config.FileGap, layout.config.PackagePadding,
		layout.config.PackageGap, layout.config.RepositoryPadding, layout.config.RepositoryGap,
		layout.config.CellSize,
	} {
		encoded = appendUint64(encoded, uint64(value))
	}
	encoded = appendUint32(encoded, uint32(len(layout.nodes)))
	for _, node := range layout.nodes {
		encoded = append(encoded, byte(node.Kind), byte(node.Level), 0, 0)
		encoded = appendUint32(encoded, uint32(node.ID))
		encoded = append(encoded, byte(node.Parent.Kind), 0, 0, 0)
		encoded = appendUint32(encoded, uint32(node.Parent.ID))
		encoded = appendRect(encoded, node.Bounds)
	}
	return encoded, nil
}

func appendRect(buffer []byte, rect Rect) []byte {
	buffer = appendUint64(buffer, uint64(rect.MinX))
	buffer = appendUint64(buffer, uint64(rect.MinY))
	buffer = appendUint64(buffer, uint64(rect.MaxX))
	return appendUint64(buffer, uint64(rect.MaxY))
}

func appendUint16(buffer []byte, value uint16) []byte {
	var encoded [2]byte
	binary.LittleEndian.PutUint16(encoded[:], value)
	return append(buffer, encoded[:]...)
}

func appendUint32(buffer []byte, value uint32) []byte {
	var encoded [4]byte
	binary.LittleEndian.PutUint32(encoded[:], value)
	return append(buffer, encoded[:]...)
}

func appendUint64(buffer []byte, value uint64) []byte {
	var encoded [8]byte
	binary.LittleEndian.PutUint64(encoded[:], value)
	return append(buffer, encoded[:]...)
}
