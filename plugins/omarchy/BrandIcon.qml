pragma ComponentBehavior: Bound
import QtQuick

// Integer-aligned cells keep the brand sharp at theme-scaled panel sizes.
Item {
  id: root
  property color foreground: "white"
  property bool fullColor: false
  property bool compact: false
  readonly property var regularRows: ["....GGGG....", "...GGGGGG...", "..GGGGWWWWW.", "..GGGWWWWWBB", ".GGGWWWEWBBB", ".GGGWWWEWBBB", "GGGGWWWWWBBB", "GGGGWWWWWBBB", "GGGGWWWW.BBB", "GGGGWWW...B.", "GGGGWWW.....", "GGGGWWW.....", "GGGGWW......", ".GGGWW......", "..GGWW......", "...GWW......"]
  // Exact opaque-cell mask from the supplied 14 x 14 Omarchy bar glyph.
  // X cells inherit the bar's foreground or urgent color.
  readonly property var compactRows: ["..............", "..............", "....XXXXX.....", "......XXXX....", "....XXXXXXX...", "...XXXX.......", "....XX..X.XX..", "..XXXX....XXX.", "....XX....XXX.", "...XX....X..X.", "..XX....XX....", "..X.....X.....", "......XX......", ".............."]
  readonly property var rows: compact ? compactRows : regularRows
  readonly property int gridWidth: compact ? 14 : 12
  readonly property int gridHeight: rows.length
  readonly property int cell: Math.max(1, Math.floor(Math.min(width / gridWidth, height / gridHeight)))
  implicitWidth: 12
  implicitHeight: gridHeight

  Item {
    width: root.cell * root.gridWidth
    height: root.cell * root.gridHeight
    anchors.centerIn: parent
    Repeater {
      model: root.gridWidth * root.gridHeight
      Rectangle {
        required property int index
        readonly property int row: Math.floor(index / root.gridWidth)
        readonly property int column: index % root.gridWidth
        readonly property string pixel: root.rows[row].charAt(column)
        readonly property bool seam: column > 0 && pixel === "W" && root.rows[row].charAt(column - 1) === "G"
        x: column * root.cell
        y: row * root.cell
        width: root.cell
        height: root.cell
        visible: pixel !== "." && (root.fullColor || (!seam && pixel !== "E"))
        color: pixel === "X" ? root.foreground : (root.fullColor ? (pixel === "G" ? "#55f536" : (pixel === "W" ? "#f5f5ed" : (pixel === "E" ? "#101315" : "#293632"))) : root.foreground)
      }
    }
  }
}
