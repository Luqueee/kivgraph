package webapi

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"unicode/utf8"

	"github.com/Luqueee/ladygraph/internal/hotsnapshot"
	"github.com/Luqueee/ladygraph/internal/layout"
)

const (
	// Version 2 appends a label section: the display name of every node, in
	// node order. Dense IDs alone are unreadable, and a second round trip per
	// tile to resolve 2.000 names is worse than the bytes.
	viewerBinaryVersion    uint16 = 2
	viewerBinaryHeaderSize        = 64
	viewerBinaryNodeSize          = 48
	viewerBinaryEdgeSize          = 16
	maxViewerPayloadBytes         = 32 << 20
	maxViewerLabelBytes           = 512
)

const (
	viewerPayloadTiles        byte = 1
	viewerPayloadNeighborhood byte = 2
)

// viewerEdgeFlagPackage marks an edge whose endpoints are packages, not
// symbols. The client resolves endpoints by node kind, so the flag is what
// separates a package relation from a symbol relation in the same section.
const viewerEdgeFlagPackage byte = 1 << 0

var (
	errViewerPayloadTooLarge = errors.New("viewer binary payload exceeds the configured limit")
	errViewerBinaryVersion   = errors.New("viewer binary version is unsupported")
	errViewerSnapshotInvalid = errors.New("viewer binary snapshot is inconsistent")
)

type binaryNodeRecord struct {
	ID         uint32
	ParentID   uint32
	Kind       layout.NodeKind
	Level      layout.LOD
	ParentKind layout.NodeKind
	Depth      uint32
	Bounds     layout.Rect
	Label      string
}

type binaryEdgeRecord struct {
	Source     uint32
	Target     uint32
	Evidence   uint32
	Kind       uint8
	Confidence uint8
	Provenance uint8
	Flags      uint8
}

func encodeTilePayload(ctx context.Context, snapshot *hotsnapshot.GraphSnapshot, nodes []layout.Node, truncated bool, level layout.LOD) ([]byte, error) {
	records := make([]binaryNodeRecord, 0, len(nodes))
	visible := make(map[hotsnapshot.SymbolID]struct{})
	visiblePackages := make(map[hotsnapshot.PackageID]struct{})
	for _, node := range nodes {
		records = append(records, binaryNodeRecord{
			ID:         uint32(node.ID),
			ParentID:   uint32(node.Parent.ID),
			Kind:       node.Kind,
			Level:      node.Level,
			ParentKind: node.Parent.Kind,
			Bounds:     node.Bounds,
			Label:      nodeLabel(snapshot, node.Kind, uint32(node.ID)),
		})
		switch node.Kind {
		case layout.NodeSymbol:
			visible[hotsnapshot.SymbolID(node.ID)] = struct{}{}
		case layout.NodePackage:
			visiblePackages[hotsnapshot.PackageID(node.ID)] = struct{}{}
		}
	}
	edges, err := collectVisibleEdges(ctx, snapshot, visible, len(records))
	if err != nil {
		return nil, err
	}
	// A tile whose nodes are packages carries the package relations between
	// them. They live outside the symbol CSR, and without them every view
	// coarser than symbols would render as disconnected dots.
	packageEdges, err := collectPackageEdges(ctx, snapshot, visiblePackages, len(records), len(edges))
	if err != nil {
		return nil, err
	}
	edges = append(edges, packageEdges...)
	return encodeViewerPayload(snapshot, viewerPayloadTiles, level, truncated, records, edges)
}

func collectPackageEdges(
	ctx context.Context,
	snapshot *hotsnapshot.GraphSnapshot,
	visible map[hotsnapshot.PackageID]struct{},
	nodeCount int,
	usedEdges int,
) ([]binaryEdgeRecord, error) {
	if len(visible) == 0 {
		return nil, nil
	}
	maxEdges, err := maxBinaryEdgeCount(nodeCount)
	if err != nil {
		return nil, err
	}
	edges := make([]binaryEdgeRecord, 0, minBinaryInt(maxEdges-usedEdges, 64))
	for _, dependency := range snapshot.AllPackageDependencies() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if _, exists := visible[dependency.Source]; !exists {
			continue
		}
		if _, exists := visible[dependency.Target]; !exists {
			continue
		}
		if usedEdges+len(edges) >= maxEdges {
			return nil, errViewerPayloadTooLarge
		}
		edges = append(edges, binaryEdgeRecord{
			Source:     uint32(dependency.Source),
			Target:     uint32(dependency.Target),
			Kind:       dependency.Kind,
			Confidence: dependency.Confidence,
			Provenance: dependency.Provenance,
			Flags:      viewerEdgeFlagPackage,
		})
	}
	return edges, nil
}

func encodeNeighborhoodPayload(ctx context.Context, snapshot *hotsnapshot.GraphSnapshot, ids []hotsnapshot.SymbolID, truncated bool) ([]byte, error) {
	records := make([]binaryNodeRecord, 0, len(ids))
	seen := make(map[hotsnapshot.SymbolID]struct{}, len(ids))
	for _, id := range ids {
		seen[id] = struct{}{}
		records = append(records, binaryNodeRecord{
			ID:    uint32(id),
			Kind:  layout.NodeSymbol,
			Level: layout.LODSymbols,
			Label: nodeLabel(snapshot, layout.NodeSymbol, uint32(id)),
		})
	}
	edges, err := collectInducedEdges(ctx, snapshot, ids, seen, len(records))
	if err != nil {
		return nil, err
	}
	return encodeViewerPayload(snapshot, viewerPayloadNeighborhood, layout.LODSymbols, truncated, records, edges)
}

func collectVisibleEdges(ctx context.Context, snapshot *hotsnapshot.GraphSnapshot, visible map[hotsnapshot.SymbolID]struct{}, nodeCount int) ([]binaryEdgeRecord, error) {
	if len(visible) == 0 {
		return nil, nil
	}
	sources := make([]hotsnapshot.SymbolID, 0, len(visible))
	for source := range visible {
		sources = append(sources, source)
	}
	sort.Slice(sources, func(left, right int) bool { return sources[left] < sources[right] })
	return collectEdges(ctx, snapshot, sources, visible, nodeCount)
}

func collectInducedEdges(ctx context.Context, snapshot *hotsnapshot.GraphSnapshot, sources []hotsnapshot.SymbolID, visible map[hotsnapshot.SymbolID]struct{}, nodeCount int) ([]binaryEdgeRecord, error) {
	return collectEdges(ctx, snapshot, sources, visible, nodeCount)
}

func collectEdges(ctx context.Context, snapshot *hotsnapshot.GraphSnapshot, sources []hotsnapshot.SymbolID, visible map[hotsnapshot.SymbolID]struct{}, nodeCount int) ([]binaryEdgeRecord, error) {
	maxEdges, err := maxBinaryEdgeCount(nodeCount)
	if err != nil {
		return nil, err
	}
	edges := make([]binaryEdgeRecord, 0, minBinaryInt(maxEdges, 64))
	for _, source := range sources {
		start, end, ok := snapshot.CSRRange(hotsnapshot.TraversalOutgoing, source)
		if !ok {
			return nil, errViewerSnapshotInvalid
		}
		err := snapshot.VisitEdges(ctx, hotsnapshot.TraversalOutgoing, start, end, func(_ hotsnapshot.EdgeID, edge hotsnapshot.PackedEdge) error {
			if _, exists := visible[edge.Target]; !exists {
				return nil
			}
			if len(edges) >= maxEdges {
				return errViewerPayloadTooLarge
			}
			edges = append(edges, binaryEdgeRecord{
				Source:     uint32(source),
				Target:     uint32(edge.Target),
				Evidence:   uint32(edge.Evidence),
				Kind:       edge.Kind,
				Confidence: edge.Confidence,
				Provenance: edge.Provenance,
				Flags:      edge.Flags,
			})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return edges, nil
}

func maxBinaryEdgeCount(nodeCount int) (int, error) {
	if nodeCount < 0 {
		return 0, errViewerPayloadTooLarge
	}
	available := uint64(maxViewerPayloadBytes - viewerBinaryHeaderSize)
	if uint64(nodeCount) > available/uint64(viewerBinaryNodeSize) {
		return 0, errViewerPayloadTooLarge
	}
	nodeBytes := uint64(nodeCount) * uint64(viewerBinaryNodeSize)
	available -= nodeBytes
	return int(available / uint64(viewerBinaryEdgeSize)), nil
}

func encodeViewerPayload(snapshot *hotsnapshot.GraphSnapshot, kind byte, level layout.LOD, truncated bool, nodes []binaryNodeRecord, edges []binaryEdgeRecord) ([]byte, error) {
	if snapshot == nil {
		return nil, errors.New("viewer binary snapshot is nil")
	}
	metadata := snapshot.Metadata()
	if metadata.Version == 0 || metadata.SchemaVersion < 0 {
		return nil, fmt.Errorf("%w: invalid snapshot metadata", errViewerBinaryVersion)
	}
	nodeCapacity := uint64(maxViewerPayloadBytes - viewerBinaryHeaderSize)
	if uint64(len(nodes)) > nodeCapacity/uint64(viewerBinaryNodeSize) {
		return nil, errViewerPayloadTooLarge
	}
	nodeBytes := uint64(len(nodes)) * uint64(viewerBinaryNodeSize)
	nodeCapacity -= nodeBytes
	if uint64(len(edges)) > nodeCapacity/uint64(viewerBinaryEdgeSize) {
		return nil, errViewerPayloadTooLarge
	}
	edgeBytes := uint64(len(edges)) * uint64(viewerBinaryEdgeSize)
	labels, labelBytes, err := encodeViewerLabels(nodes)
	if err != nil {
		return nil, err
	}
	nodeOffset := uint64(viewerBinaryHeaderSize)
	edgeOffset := nodeOffset + nodeBytes
	labelOffset := edgeOffset + edgeBytes
	totalBytes := labelOffset + labelBytes
	if totalBytes > uint64(maxViewerPayloadBytes) || totalBytes > uint64(^uint32(0)) {
		return nil, errViewerPayloadTooLarge
	}
	payload := make([]byte, int(totalBytes))
	copy(payload[:4], []byte("LGVB"))
	binary.LittleEndian.PutUint16(payload[4:6], viewerBinaryVersion)
	payload[6] = kind
	if truncated {
		payload[7] = 1
	}
	binary.LittleEndian.PutUint64(payload[8:16], metadata.ID)
	binary.LittleEndian.PutUint32(payload[16:20], uint32(len(nodes)))
	binary.LittleEndian.PutUint32(payload[20:24], uint32(len(edges)))
	binary.LittleEndian.PutUint32(payload[24:28], uint32(nodeOffset))
	binary.LittleEndian.PutUint32(payload[28:32], uint32(nodeBytes))
	binary.LittleEndian.PutUint32(payload[32:36], uint32(edgeOffset))
	binary.LittleEndian.PutUint32(payload[36:40], uint32(edgeBytes))
	payload[40] = byte(level)
	binary.LittleEndian.PutUint32(payload[44:48], uint32(totalBytes))
	binary.LittleEndian.PutUint32(payload[48:52], metadata.Version)
	binary.LittleEndian.PutUint32(payload[52:56], uint32(metadata.SchemaVersion))
	binary.LittleEndian.PutUint32(payload[56:60], uint32(labelOffset))
	binary.LittleEndian.PutUint32(payload[60:64], uint32(labelBytes))

	for index, node := range nodes {
		offset := int(nodeOffset) + index*viewerBinaryNodeSize
		binary.LittleEndian.PutUint32(payload[offset:offset+4], node.ID)
		binary.LittleEndian.PutUint32(payload[offset+4:offset+8], node.ParentID)
		payload[offset+8] = byte(node.Kind)
		payload[offset+9] = byte(node.Level)
		payload[offset+10] = byte(node.ParentKind)
		binary.LittleEndian.PutUint32(payload[offset+12:offset+16], node.Depth)
		putCoord(payload[offset+16:offset+24], node.Bounds.MinX)
		putCoord(payload[offset+24:offset+32], node.Bounds.MinY)
		putCoord(payload[offset+32:offset+40], node.Bounds.MaxX)
		putCoord(payload[offset+40:offset+48], node.Bounds.MaxY)
	}
	for index, edge := range edges {
		offset := int(edgeOffset) + index*viewerBinaryEdgeSize
		binary.LittleEndian.PutUint32(payload[offset:offset+4], edge.Source)
		binary.LittleEndian.PutUint32(payload[offset+4:offset+8], edge.Target)
		binary.LittleEndian.PutUint32(payload[offset+8:offset+12], edge.Evidence)
		payload[offset+12] = edge.Kind
		payload[offset+13] = edge.Confidence
		payload[offset+14] = edge.Provenance
		payload[offset+15] = edge.Flags
	}
	copy(payload[labelOffset:], labels)
	return payload, nil
}

// encodeViewerLabels serialises one display name per node, in node order, as a
// uint16 length followed by its UTF-8 bytes. Names are truncated at
// maxViewerLabelBytes so one pathological signature cannot inflate a tile.
func encodeViewerLabels(nodes []binaryNodeRecord) ([]byte, uint64, error) {
	total := 0
	for index := range nodes {
		total += 2 + len(truncateLabel(nodes[index].Label))
	}
	if uint64(total) > uint64(maxViewerPayloadBytes) {
		return nil, 0, errViewerPayloadTooLarge
	}
	labels := make([]byte, 0, total)
	for index := range nodes {
		label := truncateLabel(nodes[index].Label)
		labels = binary.LittleEndian.AppendUint16(labels, uint16(len(label)))
		labels = append(labels, label...)
	}
	return labels, uint64(len(labels)), nil
}

// truncateLabel keeps a label within the wire limit without splitting a rune.
func truncateLabel(label string) string {
	if len(label) <= maxViewerLabelBytes {
		return label
	}
	cut := maxViewerLabelBytes
	for cut > 0 && !utf8.RuneStart(label[cut]) {
		cut--
	}
	return label[:cut]
}

// nodeLabel resolves the display name of one layout node from the snapshot.
// An ID the snapshot does not know is reported as such instead of guessed: a
// blank label would hide an inconsistency between layout and snapshot.
func nodeLabel(snapshot *hotsnapshot.GraphSnapshot, kind layout.NodeKind, id uint32) string {
	table := snapshot.Strings()
	resolve := func(interned hotsnapshot.InternedString) string {
		value, _ := table.String(interned)
		return value
	}
	switch kind {
	case layout.NodeRepository:
		if record, found := snapshot.Repository(hotsnapshot.RepositoryID(id)); found {
			return resolve(record.Name)
		}
	case layout.NodePackage:
		if record, found := snapshot.Package(hotsnapshot.PackageID(id)); found {
			return resolve(record.Name)
		}
	case layout.NodeFile:
		if record, found := snapshot.File(hotsnapshot.FileID(id)); found {
			return resolve(record.Path)
		}
	case layout.NodeSymbol:
		if record, found := snapshot.Symbol(hotsnapshot.SymbolID(id)); found {
			if name := resolve(record.QualifiedName); name != "" {
				return name
			}
			return resolve(record.Name)
		}
	}
	return fmt.Sprintf("unknown %d", id)
}

func putCoord(destination []byte, value layout.Coord) {
	binary.LittleEndian.PutUint64(destination, uint64(value))
}

func minBinaryInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

// validateViewerBinaryVersion accepts only the version this server emits. A
// client pinned to version 1 cannot parse the label section, so answering it
// with a v2 payload would be a silent format break.
func validateViewerBinaryVersion(requestVersion string) error {
	if requestVersion == "" || requestVersion == "2" {
		return nil
	}
	return errViewerBinaryVersion
}
