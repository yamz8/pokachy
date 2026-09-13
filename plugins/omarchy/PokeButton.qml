import QtQuick
import QtQuick.Shapes
import qs.Commons
import qs.Ui

Item {
  id: root
  property bool sending: false
  property bool incoming: false
  property bool waiting: false
  property bool busy: false
  property string contactHandle: ""
  property Item focusFallback: null
  property bool handHovered: false
  readonly property bool waitingOnly: waiting && !incoming
  property real size: Style.space(36)
  property string fontFamily: Style.font.family
  property color foreground: incoming ? Color.accent : Color.foreground
  signal clicked()
  implicitWidth: size
  implicitHeight: size
  readonly property bool actionable: !busy && !sending && !waitingOnly
  activeFocusOnTab: actionable
  onActionableChanged: {
    handHovered = false
    if (!actionable && activeFocus && focusFallback)
      focusFallback.forceActiveFocus()
  }
  Keys.onReturnPressed: if (actionable) clicked()
  Keys.onEnterPressed: if (actionable) clicked()
  Keys.onSpacePressed: if (actionable) clicked()
  // Sending wins visually; incoming keeps reply available when both directions exist.
  property string tooltipText: sending ? "Sending poke…" : (busy ? "Another action is in progress" : (incoming ? "Poke @" + contactHandle + " back" : (waiting ? "Poke sent · waiting for a poke back" : "Poke @" + contactHandle)))
  Accessible.role: Accessible.Button
  Accessible.name: tooltipText
  Accessible.focusable: actionable
  Accessible.description: actionable ? "" : "Temporarily unavailable"
  Accessible.onPressAction: if (actionable) root.clicked()

  PanelActionButton {
    id: actionSurface
    anchors.fill: parent
    radius: 0
    enabled: root.actionable
    hasCursor: root.activeFocus
    borderSpec: root.activeFocus ? Border.controlSpec("focus", root.foreground, root.foreground) : Border.none()
    color: root.activeFocus ? Style.focusFillFor(root.foreground, root.foreground) : "transparent"
    Accessible.ignored: true
    onHovered: function(hot) { root.handHovered = hot }
    foreground: root.foreground
    tooltipText: root.tooltipText
    fontFamily: root.fontFamily
    onClicked: { root.forceActiveFocus(); root.clicked() }
  }
  PokeHand {
    anchors.centerIn: parent
    width: Style.space(20)
    height: width
    visible: !root.sending
    foreground: !root.actionable ? Color.muted : (root.handHovered ? Qt.rgba(root.foreground.r, root.foreground.g, root.foreground.b, 0.8) : root.foreground)
  }
  Item {
    id: sendingSpinner
    anchors.centerIn: parent
    width: Style.space(22)
    height: width
    visible: root.sending
    Shape {
      anchors.fill: parent
      preferredRendererType: Shape.CurveRenderer
      ShapePath {
        fillColor: "transparent"
        strokeColor: root.foreground
        strokeWidth: Style.space(2)
        capStyle: ShapePath.RoundCap
        PathAngleArc {
          centerX: sendingSpinner.width / 2
          centerY: sendingSpinner.height / 2
          radiusX: sendingSpinner.width / 2 - Style.space(3)
          radiusY: radiusX
          startAngle: 0
          sweepAngle: 270
        }
      }
    }
    RotationAnimator on rotation {
      from: 0
      to: 360
      duration: 900
      loops: Animation.Infinite
      running: root.sending
    }
  }
  Rectangle {
    visible: !root.sending && (root.incoming || root.waitingOnly)
    anchors.right: parent.right
    anchors.top: parent.top
    anchors.margins: Style.space(3)
    width: root.incoming ? Style.space(8) : Style.space(12)
    height: width
    radius: width / 2
    color: root.incoming ? Color.accent : Color.popups.background
    border.width: Style.space(1)
    border.color: Color.popups.background
    Text {
      visible: root.waitingOnly
      anchors.centerIn: parent
      text: "󰥔"
      color: Color.muted
      font.family: root.fontFamily
      font.pixelSize: Style.space(11)
    }
  }
  // The toolkit disables its own hover area along with the button. Keep state
  // explanations available for disabled actions without intercepting clicks.
  MouseArea {
    id: disabledHover
    anchors.fill: parent
    enabled: !root.actionable
    hoverEnabled: true
    acceptedButtons: Qt.NoButton
  }
  PanelToolTip {
    visible: disabledHover.containsMouse
    text: root.tooltipText
    fontFamily: root.fontFamily
  }
}
