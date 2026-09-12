import QtQuick
import QtQuick.Effects
import qs.Commons
import qs.Ui

// The bar entry owns the shell-facing popup contract. Panel.qml is loaded
// separately so its long-running cache watcher survives opening and closing.
BarWidget {
  id: root
  moduleName: "com.pokachy.poke"

  readonly property bool opened: panelLoader.item ? panelLoader.item.opened === true : false
  readonly property bool popoutSwitchClosing: panelLoader.item ? panelLoader.item.popoutSwitchClosing === true : false

  function injectPanel() {
    var target = panelLoader.item
    if (!target) return
    target.bar = root.bar
    target.settings = root.settings
    target.anchorItem = button
    target.hostWidget = root
  }

  function open() { if (panelLoader.item) panelLoader.item.open() }
  function close() { if (panelLoader.item) panelLoader.item.close() }
  function closeForPopoutSwitch() { if (panelLoader.item) panelLoader.item.closeForPopoutSwitch() }
  function refresh() { if (panelLoader.item) panelLoader.item.refresh() }

  implicitWidth: button.implicitWidth
  implicitHeight: button.implicitHeight
  onBarChanged: injectPanel()
  onSettingsChanged: injectPanel()

  Loader {
    id: panelLoader
    active: true
    source: Qt.resolvedUrl("Panel.qml")
    visible: false
    onLoaded: { root.injectPanel(); Qt.callLater(root.injectPanel) }
  }

  BarIconButton {
    id: button
    anchors.fill: parent
    bar: root.bar
    slotSize: Style.bar.statusSlot
    iconComponent: Component {
      Item {
        readonly property real iconSize: Math.round(Math.min(width, height) * 0.75)

        Image {
          id: pokachyIconSource
          anchors.centerIn: parent
          width: parent.iconSize
          height: parent.iconSize
          source: Qt.resolvedUrl("pokachy-bar-icon.png")
          fillMode: Image.PreserveAspectFit
          smooth: false
          visible: false
          layer.enabled: true
        }
        MultiEffect {
          anchors.fill: pokachyIconSource
          source: pokachyIconSource
          colorization: 1.0
          colorizationColor: button.active && button.useActiveColor ? button.activeColor : button.foreground
          opacity: panelLoader.item && panelLoader.item.needsLogin ? 0.55 : 1.0
        }
      }
    }
    active: panelLoader.item && panelLoader.item.inboxCount > 0
    tooltipText: panelLoader.item ? panelLoader.item.barTooltip : "Pokachy"
    onPressed: function(mouseButton) {
      if (mouseButton === Qt.MiddleButton) root.refresh()
      else if (panelLoader.item) panelLoader.item.toggle()
    }
  }
}
