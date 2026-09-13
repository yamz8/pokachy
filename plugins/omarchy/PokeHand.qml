import QtQuick
import QtQuick.Shapes

// Monochrome filled index-up silhouette; independent of emoji/font coverage.
Item {
  id: root
  property color foreground: "white"
  implicitWidth: 20
  implicitHeight: 20
  Shape {
    anchors.fill: parent
    preferredRendererType: Shape.CurveRenderer
    ShapePath {
      fillColor: root.foreground
      strokeColor: "transparent"
      PathSvg { path: "M 8,19 L 5,14 Q 4,12 3,10 Q 2,8 3.5,7.5 Q 4.5,7 5.5,9 L 7,11 L 7,2 Q 7,0 8.5,0 Q 10,0 10,2 L 10,8 Q 12,6.5 13,8 Q 15,7 16,9 Q 18,8.5 18,11 L 18,14 L 16,19 Z" }
    }
    transform: Scale { xScale: root.width / 20; yScale: root.height / 20 }
  }
}
