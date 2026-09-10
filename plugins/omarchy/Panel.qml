import QtQuick
import QtQuick.Controls
import Quickshell
import Quickshell.Io
import qs.Commons
import qs.Ui

Panel {
  id: root
  moduleName: "com.pokachy.poke"
  ipcTarget: "com.pokachy.poke"
  manageIpc: false

  property var anchorItem: null
  property var hostWidget: null
  readonly property var barIdentity: hostWidget || root
  property var pokachyState: ({ needsLogin: true })
  property string actionError: ""
  property string actionNotice: ""
  property string statusError: ""
  readonly property string lastError: actionError !== "" ? actionError : statusError
  property string actionLabel: ""
  property string actionKey: ""
  readonly property bool actionRunning: actionProc.running
  readonly property bool needsLogin: pokachyState && pokachyState.needsLogin === true
  readonly property bool online: pokachyState && pokachyState.online === true
  readonly property int inboxCount: pokachyState && pokachyState.inbox ? pokachyState.inbox.length : 0
  readonly property color foreground: bar ? bar.foreground : Color.foreground
  readonly property string fontFamily: bar ? bar.fontFamily : Style.font.family
  readonly property string barTooltip: needsLogin ? "Pokachy · connect this computer"
    : (inboxCount > 0 ? "Pokachy · " + inboxCount + " waiting" : "Pokachy · " + (online ? "online" : "offline"))

  function list(name) {
    return pokachyState && pokachyState[name] instanceof Array ? pokachyState[name] : []
  }
  function displayName(person) {
    if (!person) return "Someone"
    var name = String(person.name || "").trim()
    var handle = String(person.handle || "").trim()
    return name !== "" ? name : (handle !== "" ? "@" + handle.replace(/^@/, "") : "Someone")
  }
  function handle(person) {
    return String(person && person.handle || "").replace(/^@/, "")
  }
  function parseState(line) {
    try {
      var next = JSON.parse(String(line))
      if (next && typeof next === "object" && (next.needsLogin !== undefined || (next.me && typeof next.me === "object"))) {
        pokachyState = next
        statusError = ""
      }
    } catch (error) {
      statusError = "Could not read Pokachy status."
    }
  }
  function refresh() {
    if (!statusProc.running) {
      statusProc.command = ["/usr/bin/env", "pokachy", "status", "--json"]
      statusProc.running = true
    }
  }
  function runAction(label, args) {
    if (actionProc.running) return
    actionError = ""
    actionNotice = ""
    actionNoticeTimer.stop()
    actionLabel = label
    actionKey = args.join(":")
    actionProc.command = ["/usr/bin/env", "pokachy"].concat(args)
    actionProc.running = true
  }
  function addFriend() {
    var value = friendField.text.trim()
    if (value === "") return
    runAction("Adding friend…", ["friends", "add", value])
    friendField.text = ""
  }
  function setup() {
    // This command is fully static. No state, token, or user supplied input
    // is passed through the shell launcher.
    if (bar) bar.run("omarchy-launch-floating-terminal-with-presentation pokachy init")
  }
  function open() { refresh(); controller.show() }
  function close() { controller.hide() }
  function toggle() { opened ? close() : open() }
  function closeForPopoutSwitch() { root.popoutSwitchClosing = true; close(); Qt.callLater(function() { root.popoutSwitchClosing = false }) }
  function switchPanel(direction) {
    return bar && typeof bar.switchPanelFrom === "function" ? bar.switchPanelFrom(barIdentity, direction) : false
  }

  Process {
    id: watchProc
    running: true
    command: ["/usr/bin/env", "pokachy", "watch", "--json"]
    stdout: SplitParser { onRead: function(line) { root.parseState(line) } }
    stderr: SplitParser { onRead: function(line) { root.statusError = "Pokachy is unavailable. Check that the CLI is installed." } }
    onExited: function(code) {
      if (code !== 0 && root.statusError === "") root.statusError = "Pokachy is unavailable. Check that the CLI is installed."
      if (code !== 0) watchRestart.restart()
    }
  }
  Timer { id: watchRestart; interval: 3000; onTriggered: if (!watchProc.running) watchProc.running = true }
  Process {
    id: statusProc
    command: []
    stdout: SplitParser { onRead: function(line) { root.parseState(line) } }
    stderr: SplitParser { onRead: function(line) { root.statusError = "Pokachy is unavailable. Check that the CLI is installed." } }
    onExited: function(code) { if (code !== 0 && root.statusError === "") root.statusError = "Could not refresh Pokachy." }
  }
  Process {
    id: actionProc
    command: []
    // Action commands write human confirmation such as "Done.", not State
    // JSON. The status command and the watcher own state updates.
    stdout: SplitParser {
      onRead: function(line) {
        var message = String(line).trim()
        if (message !== "") {
          root.actionNotice = message
          actionNoticeTimer.restart()
        }
      }
    }
    stderr: SplitParser { onRead: function(line) { root.actionError = String(line).trim() } }
    onExited: function(code) {
      if (code !== 0 && root.actionError === "") root.actionError = root.actionLabel + " failed."
      if (code === 0 && root.actionNotice === "") {
        root.actionNotice = "Done."
        actionNoticeTimer.restart()
      }
      root.actionLabel = ""
      root.refresh()
    }
  }
  Timer { id: actionNoticeTimer; interval: 3500; onTriggered: root.actionNotice = "" }

  IpcHandler {
    target: root.ipcTarget
    function open(): void { root.open() }
    function close(): void { root.close() }
    function toggle(): void { root.toggle() }
    function refresh(): void { root.refresh() }
  }

  KeyboardPanel {
    id: panel
    anchorItem: root.anchorItem
    owner: root.barIdentity
    bar: root.bar
    open: root.opened
    focusTarget: keyCatcher
    contentWidth: panel.fittedContentWidth(Style.space(430), Style.space(520))
    contentHeight: panel.fittedContentHeight(content.implicitHeight, Style.space(650))

    PanelKeyCatcher {
      id: keyCatcher
      anchors.fill: parent
      // This panel contains standard focusable controls, so leave their key
      // events alone and let Qt's native Tab chain drive navigation.
      blocked: true
      onCloseRequested: root.close()

      Shortcut { sequence: "Escape"; enabled: root.opened; onActivated: root.close() }

      Flickable {
        anchors.fill: parent
        contentWidth: width
        contentHeight: content.implicitHeight
        clip: true
        boundsBehavior: Flickable.StopAtBounds
        interactive: contentHeight > height

        Column {
          id: content
          width: parent.width
          spacing: Style.space(12)

          PanelHero {
            width: parent.width
            foreground: root.foreground
            fontFamily: root.fontFamily
            title: root.needsLogin ? "Connect Pokachy" : (root.pokachyState.me && root.pokachyState.me.handle ? "@" + root.pokachyState.me.handle : "Pokachy")
            meta: root.needsLogin ? "A little nudge for your Linux friends" : (root.online ? "Connected" : "Offline · showing cached activity")
            detail: root.needsLogin ? "SETUP" : (root.inboxCount > 0 ? root.inboxCount + " WAITING" : (root.online ? "ONLINE" : "OFFLINE"))
            iconComponent: Component {
              Text { text: root.needsLogin ? "󰍹" : "󰍴"; color: root.inboxCount > 0 ? (root.bar ? root.bar.urgent : Color.urgent) : root.foreground; font.family: root.fontFamily; font.pixelSize: Style.font.displayLarge }
            }
          }

          Rectangle {
            visible: root.lastError !== ""
            width: parent.width
            implicitHeight: errorText.implicitHeight + Style.space(16)
            radius: Style.cornerRadius
            color: Style.hoverFillFor(root.bar ? root.bar.urgent : Color.urgent, root.bar ? root.bar.urgent : Color.urgent)
            Text { id: errorText; anchors.fill: parent; anchors.margins: Style.space(8); text: root.lastError; textFormat: Text.PlainText; wrapMode: Text.Wrap; color: root.foreground; font.family: root.fontFamily; font.pixelSize: Style.font.body }
            MouseArea { anchors.fill: parent; cursorShape: Qt.PointingHandCursor; onClicked: { root.actionError = ""; root.statusError = "" } }
          }
          Text {
            visible: root.actionNotice !== ""
            width: parent.width
            text: root.actionNotice
            textFormat: Text.PlainText
            wrapMode: Text.Wrap
            color: Qt.darker(root.foreground, 1.25)
            font.family: root.fontFamily
            font.pixelSize: Style.font.body
          }

          Column {
            visible: root.needsLogin
            width: parent.width
            spacing: Style.space(10)
            Text { width: parent.width; text: "Set up this computer in a terminal, approve the device code in your browser, then Pokachy will appear here."; wrapMode: Text.Wrap; color: Qt.darker(root.foreground, 1.35); font.family: root.fontFamily; font.pixelSize: Style.font.body }
            Button { text: "Open setup"; focusable: true; onClicked: root.setup() }
          }

          Column {
            visible: !root.needsLogin
            width: parent.width
            spacing: Style.space(12)

            Row {
              width: parent.width
              spacing: Style.space(8)
              Text { text: root.pokachyState.me && root.pokachyState.me.quiet ? "󰂛  Quiet mode" : "󰂚  Notifications on"; color: root.foreground; font.family: root.fontFamily; font.pixelSize: Style.font.body; anchors.verticalCenter: parent.verticalCenter }
              Item { width: Math.max(0, parent.width - parent.children[0].implicitWidth - quietButton.implicitWidth - Style.space(8)); height: 1 }
              Button { id: quietButton; text: root.pokachyState.me && root.pokachyState.me.quiet ? "Resume" : "Quiet"; focusable: true; onClicked: root.runAction("Updating quiet mode…", ["quiet", root.pokachyState.me && root.pokachyState.me.quiet ? "off" : "on"]) }
            }

            PanelSeparator { foreground: root.foreground }
            PanelSectionHeader { text: "Inbox"; foreground: root.foreground; fontFamily: root.fontFamily }
            Text { visible: root.list("inbox").length === 0; text: "All caught up."; color: Qt.darker(root.foreground, 1.45); font.family: root.fontFamily; font.pixelSize: Style.font.body }
            Repeater {
              model: root.list("inbox")
              delegate: personRow
            }

            PanelSeparator { foreground: root.foreground }
            PanelSectionHeader { text: "Friends"; foreground: root.foreground; fontFamily: root.fontFamily }
            Row {
              width: parent.width
              spacing: Style.space(8)
              TextField { id: friendField; width: Math.max(Style.space(180), parent.width - addButton.implicitWidth - Style.space(8)); placeholderText: "Add @handle"; activeFocusOnTab: true; onAccepted: root.addFriend() }
              Button { id: addButton; text: "Add"; focusable: true; onClicked: root.addFriend() }
            }
            Repeater { model: root.list("friends"); delegate: friendRow }
            Text { visible: root.list("friends").length === 0; text: "Add a friend to send your first poke."; color: Qt.darker(root.foreground, 1.45); font.family: root.fontFamily; font.pixelSize: Style.font.body }

            PanelSectionHeader { visible: root.list("requests").length > 0; text: "Requests"; foreground: root.foreground; fontFamily: root.fontFamily }
            Repeater { model: root.list("requests"); delegate: requestRow }

            PanelSeparator { visible: root.list("blocked").length > 0; foreground: root.foreground }
            PanelSectionHeader { visible: root.list("blocked").length > 0; text: "Blocked"; foreground: root.foreground; fontFamily: root.fontFamily }
            Repeater { model: root.list("blocked"); delegate: blockedRow }
          }
        }
      }
    }
  }

  Component {
    id: personRow
    Item {
      required property var modelData
      width: content.width; height: Math.max(Style.space(38), name.implicitHeight + Style.space(10))
      Text { id: name; anchors.left: parent.left; anchors.verticalCenter: parent.verticalCenter; width: Math.max(Style.space(120), parent.width - rowActions.implicitWidth - Style.space(8)); text: root.displayName(modelData); textFormat: Text.PlainText; elide: Text.ElideRight; color: root.foreground; font.family: root.fontFamily; font.pixelSize: Style.font.body }
      Row { id: rowActions; anchors.right: parent.right; anchors.verticalCenter: parent.verticalCenter; spacing: Style.space(4)
        Button { text: "Back"; focusable: true; onClicked: root.runAction("Sending poke…", ["poke", "@" + root.handle(modelData)]) }
        Button { text: "Dismiss"; focusable: true; onClicked: root.runAction("Dismissing poke…", ["dismiss", String(modelData.id)]) }
        PanelActionButton { iconText: "󰅖"; foreground: root.foreground; hoverColor: root.bar ? root.bar.urgent : Color.urgent; tooltipText: "Block"; focusable: true; onClicked: root.runAction("Blocking…", ["block", "@" + root.handle(modelData)]) }
      }
    }
  }
  Component {
    id: friendRow
    Item {
      required property var modelData
      readonly property string friendHandle: root.handle(modelData)
      readonly property bool waitingForReply: Number(modelData.waiting) === 1
      readonly property bool sendingPoke: root.actionRunning && root.actionKey === "poke:@" + friendHandle
      width: content.width; height: Math.max(Style.space(38), name.implicitHeight + Style.space(10))
      Text { id: name; anchors.left: parent.left; anchors.verticalCenter: parent.verticalCenter; width: Math.max(Style.space(120), parent.width - rowActions.implicitWidth - Style.space(8)); text: root.displayName(modelData); textFormat: Text.PlainText; elide: Text.ElideRight; color: root.foreground; font.family: root.fontFamily; font.pixelSize: Style.font.body }
      Row { id: rowActions; anchors.right: parent.right; anchors.verticalCenter: parent.verticalCenter; spacing: Style.space(4)
        Button { text: sendingPoke ? "Sending…" : (waitingForReply ? "Waiting" : "Poke"); tooltipText: waitingForReply ? "Waiting for @" + friendHandle + " to answer" : ""; enabled: !waitingForReply && !root.actionRunning; focusable: true; onClicked: root.runAction("Sending poke…", ["poke", "@" + friendHandle]) }
        PanelActionButton { iconText: "󰆴"; foreground: root.foreground; hoverColor: root.bar ? root.bar.urgent : Color.urgent; tooltipText: "Remove friend"; focusable: true; onClicked: root.runAction("Removing friend…", ["friends", "remove", "@" + root.handle(modelData)]) }
        PanelActionButton { iconText: "󰅖"; foreground: root.foreground; hoverColor: root.bar ? root.bar.urgent : Color.urgent; tooltipText: "Block"; focusable: true; onClicked: root.runAction("Blocking…", ["block", "@" + root.handle(modelData)]) }
      }
    }
  }
  Component {
    id: requestRow
    Item {
      required property var modelData
      width: content.width; height: Math.max(Style.space(38), name.implicitHeight + Style.space(10))
      Text { id: name; anchors.left: parent.left; anchors.verticalCenter: parent.verticalCenter; width: Math.max(Style.space(120), parent.width - requestActions.implicitWidth - Style.space(8)); text: root.displayName(modelData); textFormat: Text.PlainText; elide: Text.ElideRight; color: root.foreground; font.family: root.fontFamily; font.pixelSize: Style.font.body }
      Row { id: requestActions; anchors.right: parent.right; anchors.verticalCenter: parent.verticalCenter; spacing: Style.space(4)
        Button { visible: modelData.outgoing !== 1; text: "Accept"; focusable: true; onClicked: root.runAction("Accepting request…", ["friends", "accept", "@" + root.handle(modelData)]) }
        Button { text: modelData.outgoing === 1 ? "Cancel" : "Decline"; focusable: true; onClicked: root.runAction(modelData.outgoing === 1 ? "Canceling request…" : "Declining request…", ["friends", "remove", "@" + root.handle(modelData)]) }
      }
    }
  }
  Component {
    id: blockedRow
    Item {
      required property var modelData
      width: content.width; height: Math.max(Style.space(38), name.implicitHeight + Style.space(10))
      Text { id: name; anchors.left: parent.left; anchors.verticalCenter: parent.verticalCenter; width: Math.max(Style.space(120), parent.width - action.implicitWidth - Style.space(8)); text: root.displayName(modelData); textFormat: Text.PlainText; elide: Text.ElideRight; color: root.foreground; font.family: root.fontFamily; font.pixelSize: Style.font.body }
      Button { id: action; anchors.right: parent.right; anchors.verticalCenter: parent.verticalCenter; text: "Unblock"; focusable: true; onClicked: root.runAction("Unblocking…", ["unblock", "@" + root.handle(modelData)]) }
    }
  }
}
