pragma ComponentBehavior: Bound
import QtQuick

// Integer-aligned cells keep the brand sharp at theme-scaled panel sizes.
Item {
  id: root
  property color foreground: "white"
  property bool fullColor: false
  readonly property var rows: ["....GGGG........", "...GGGGGG.......", "..GGGGWWWWW.....", "..GGGWWWWWBB....", ".GGGWWWEWBBB....", ".GGGWWWEWBBB....", "GGGGWWWWWBBB....", "GGGGWWWWWBBB....", "GGGGWWWW.BBB....", "GGGGWWW...B.....", "GGGGWWW.........", "GGGGWWW.........", "GGGGWW..........", ".GGGWW..........", "..GGWW..........", "...GWW.........."]
  readonly property int cell: Math.max(1, Math.floor(Math.min(width / 12, height / 16)))
  implicitWidth: 12
  implicitHeight: 16
  Item {
    width: root.cell * 12
    height: root.cell * 16
    anchors.centerIn: parent
    Repeater {
      model: 256
      Rectangle {
        required property int index
        readonly property int row: Math.floor(index / 16)
        readonly property int column: index % 16
        readonly property string pixel: root.rows[row].charAt(column)
        readonly property bool seam: column > 0 && pixel === "W" && root.rows[row].charAt(column - 1) === "G"
        x: column * root.cell
        y: row * root.cell
        width: root.cell
        height: root.cell
        visible: pixel !== "." && (root.fullColor || (!seam && pixel !== "E"))
        color: root.fullColor ? (pixel === "G" ? "#55f536" : (pixel === "W" ? "#f5f5ed" : (pixel === "E" ? "#101315" : "#293632"))) : root.foreground
      }
    }
  }
}
