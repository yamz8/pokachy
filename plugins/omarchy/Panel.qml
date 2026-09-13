import QtQuick
import QtQuick.Controls
import QtQuick.Controls as QQC
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
  property string activeFriendHandle: ""
  readonly property bool actionRunning: actionProc.running
  readonly property bool needsLogin: pokachyState && pokachyState.needsLogin === true
  readonly property bool online: pokachyState && pokachyState.online === true
  readonly property int inboxCount: pokachyState && pokachyState.inbox ? pokachyState.inbox.length : 0
  readonly property color foreground: bar ? bar.foreground : Color.foreground
  readonly property string fontFamily: bar ? bar.fontFamily : Style.font.family
  readonly property var activeFriend: friendForHandle(activeFriendHandle)
  readonly property var activeIncomingPoke: inboxFor(activeFriend)
  readonly property var activePokeHistory: historyFor(activeFriend)
  readonly property bool activeWaitingForReply: activeFriend && Number(activeFriend.waiting) === 1
  readonly property bool activeSendingPoke: activeFriend && actionRunning && actionKey === "poke:@" + handle(activeFriend)
  readonly property string barTooltip: needsLogin ? "Pokachy · connect this computer"
    : (inboxCount > 0 ? "Pokachy · " + inboxCount + " waiting" : "Pokachy · " + (online ? "online" : "offline"))

  component AvatarButton: BorderSurface {
    id: avatar
    property string label: "?"
    property string tooltipText: ""
    property color foreground: root.foreground
    property bool interactive: false
    property bool expanded: false
    property real size: Style.space(34)
    signal clicked()

    implicitWidth: size
    implicitHeight: size
    radius: size / 2
    activeFocusOnTab: interactive
    Keys.onReturnPressed: if (interactive) avatar.clicked()
    Keys.onEnterPressed: if (interactive) avatar.clicked()
    Keys.onSpacePressed: if (interactive) avatar.clicked()
    Accessible.role: interactive ? Accessible.Button : Accessible.StaticText
    Accessible.name: tooltipText
    Accessible.onPressAction: if (interactive) avatar.clicked()

    readonly property bool hot: interactive && avatarMouse.containsMouse
    color: activeFocus ? Style.focusFillFor(foreground, foreground)
      : (hot || expanded ? Style.selectedFillFor(foreground, foreground)
                         : Qt.rgba(foreground.r, foreground.g, foreground.b, 0.12))
    borderSpec: activeFocus
      ? Border.controlSpec("focus", foreground, foreground)
      : (expanded ? Border.controlSpec("selected", foreground, foreground) : Border.none())

    Text {
      anchors.centerIn: parent
      text: avatar.label
      textFormat: Text.PlainText
      color: avatar.foreground
      font.family: root.fontFamily
      font.pixelSize: Style.font.subtitle
      font.bold: true
    }

    MouseArea {
      id: avatarMouse
      anchors.fill: parent
      hoverEnabled: true
      enabled: avatar.interactive
      cursorShape: Qt.PointingHandCursor
      onClicked: {
        avatar.forceActiveFocus()
        avatar.clicked()
      }
    }

    PanelToolTip {
      visible: avatar.tooltipText !== "" && avatarMouse.containsMouse
      text: avatar.tooltipText
      fontFamily: root.fontFamily
    }
  }

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
  function friendForHandle(value) {
    var wanted = String(value || "").replace(/^@/, "")
    var friends = list("friends")
    for (var i = 0; i < friends.length; i++) {
      if (handle(friends[i]) === wanted) return friends[i]
    }
    return null
  }
  function normalizedSearch(value) {
    return String(value || "").trim().toLowerCase().replace(/^@/, "")
  }
  function filteredFriends(value) {
    var query = normalizedSearch(value)
    var friends = list("friends")
    if (query === "") return friends
    var matches = []
    for (var i = 0; i < friends.length; i++) {
      var friendHandle = handle(friends[i]).toLowerCase()
      var friendName = String(friends[i].name || "").toLowerCase()
      if (friendHandle.indexOf(query) !== -1 || friendName.indexOf(query) !== -1) matches.push(friends[i])
    }
    return matches
  }
  function exactSearchFriend(value) {
    var query = normalizedSearch(value)
    return query === "" ? null : friendForHandle(query)
  }
  function canAddSearch(value) {
    var query = normalizedSearch(value)
    return /^[a-z0-9][a-z0-9_]{2,23}$/.test(query) && filteredFriends(value).length === 0
  }
  function submitFriendSearch() {
    var exact = exactSearchFriend(friendSearch.text)
    if (exact) {
      openConversation(exact)
      return
    }
    var matches = filteredFriends(friendSearch.text)
    if (matches.length === 1) {
      openConversation(matches[0])
      return
    }
    if (canAddSearch(friendSearch.text)) addFriend(friendSearch.text)
  }
  function inboxFor(person) {
    var friendHandle = handle(person)
    var inbox = list("inbox")
    for (var i = 0; i < inbox.length; i++) {
      if (handle(inbox[i]) === friendHandle) return inbox[i]
    }
    return null
  }
  function historyFor(person) {
    var friendHandle = handle(person)
    var history = list("history")
    var matches = []
    for (var i = 0; i < history.length; i++) {
      if (handle(history[i]) === friendHandle) matches.push(history[i])
    }
    return matches.slice(0, 5).reverse()
  }
  function avatarInitial(person) {
    var source = String(person && (person.handle || person.name) || "?").trim().replace(/^@/, "")
    return source === "" ? "?" : source.charAt(0).toUpperCase()
  }
  function formatPokeTime(value) {
    var milliseconds = Number(value)
    if (!isFinite(milliseconds) || milliseconds <= 0) return ""
    return Qt.formatDateTime(new Date(milliseconds), "MMM d · HH:mm")
  }
  function openConversation(person) {
    activeFriendHandle = handle(person)
    friendSearch.text = ""
    panelScroll.contentY = 0
  }
  function closeConversation() {
    activeFriendHandle = ""
    panelScroll.contentY = 0
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
  function addFriend(value) {
    var friendHandle = normalizedSearch(value)
    if (!canAddSearch(friendHandle)) return
    runAction("Adding friend…", ["friends", "add", "@" + friendHandle])
    friendSearch.text = ""
  }
  function setup() {
    // This command is fully static. No state, token, or user supplied input
    // is passed through the shell launcher.
    if (bar) bar.run("omarchy-launch-floating-terminal-with-presentation pokachy init")
  }
  function open() { refresh(); controller.show() }
  function close() { closeConversation(); friendSearch.text = ""; controller.hide() }
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

      Shortcut { sequence: "Escape"; enabled: root.opened; onActivated: root.activeFriend ? root.closeConversation() : root.close() }

      Flickable {
        id: panelScroll
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
            visible: root.needsLogin || !root.activeFriend
            width: parent.width
            foreground: root.foreground
            fontFamily: root.fontFamily
            title: root.needsLogin ? "Connect Pokachy" : (root.pokachyState.me && root.pokachyState.me.handle ? "@" + root.pokachyState.me.handle : "Pokachy")
            meta: root.needsLogin ? "A little nudge for your Linux friends" : (root.online ? "" : "Offline · cached activity")
            detail: root.needsLogin ? "SETUP" : (root.inboxCount > 0 ? root.inboxCount + (root.inboxCount === 1 ? " POKE" : " POKES") : "")
            iconComponent: Component {
              AvatarButton {
                label: root.avatarInitial(root.pokachyState.me)
                tooltipText: root.needsLogin ? "Pokachy" : "Your profile"
                foreground: root.foreground
                interactive: false
                size: Style.space(36)
                opacity: root.needsLogin ? 0.55 : 1.0
              }
            }
            trailingControl: root.needsLogin ? null : accountActions
          }

          Component {
            id: accountActions
            PanelActionButton {
              iconText: root.pokachyState.me && root.pokachyState.me.quiet ? "󰂛" : "󰂚"
              tooltipText: root.pokachyState.me && root.pokachyState.me.quiet ? "Resume notifications" : "Quiet notifications"
              foreground: root.foreground
              fontFamily: root.fontFamily
              size: Style.space(28)
              focusable: true
              Accessible.name: tooltipText
              onClicked: root.runAction("Updating quiet mode…", ["quiet", root.pokachyState.me && root.pokachyState.me.quiet ? "off" : "on"])
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
            id: friendListView
            visible: !root.needsLogin && !root.activeFriend
            width: parent.width
            spacing: Style.space(8)

            TextField {
              id: friendSearch
              width: parent.width
              placeholderText: "Find or add @handle"
              activeFocusOnTab: true
              onAccepted: root.submitFriendSearch()
            }
            Repeater { model: root.filteredFriends(friendSearch.text); delegate: friendRow }
            Button {
              visible: root.canAddSearch(friendSearch.text)
              width: parent.width
              text: "Add @" + root.normalizedSearch(friendSearch.text)
              iconText: "󰐕"
              leftAlign: true
              focusable: true
              foreground: root.foreground
              fontFamily: root.fontFamily
              onClicked: root.addFriend(friendSearch.text)
            }
            Text {
              visible: friendSearch.text.trim() !== "" && root.filteredFriends(friendSearch.text).length === 0 && !root.canAddSearch(friendSearch.text)
              width: parent.width
              text: "No matching contact"
              horizontalAlignment: Text.AlignHCenter
              color: Qt.darker(root.foreground, 1.45)
              font.family: root.fontFamily
              font.pixelSize: Style.font.body
            }
            Text { visible: friendSearch.text.trim() === "" && root.list("friends").length === 0; text: "Search for a handle to add your first friend."; color: Qt.darker(root.foreground, 1.45); font.family: root.fontFamily; font.pixelSize: Style.font.body }

            PanelSeparator { visible: friendSearch.text.trim() === "" && root.list("requests").length > 0; foreground: root.foreground }
            PanelSectionHeader { visible: friendSearch.text.trim() === "" && root.list("requests").length > 0; text: "Requests"; foreground: root.foreground; fontFamily: root.fontFamily }
            Repeater { model: friendSearch.text.trim() === "" ? root.list("requests") : []; delegate: requestRow }

            PanelSeparator { visible: friendSearch.text.trim() === "" && root.list("blocked").length > 0; foreground: root.foreground }
            PanelSectionHeader { visible: friendSearch.text.trim() === "" && root.list("blocked").length > 0; text: "Blocked"; foreground: root.foreground; fontFamily: root.fontFamily }
            Repeater { model: friendSearch.text.trim() === "" ? root.list("blocked") : []; delegate: blockedRow }
          }

          Column {
            id: conversationView
            visible: !root.needsLogin && !!root.activeFriend
            width: parent.width
            spacing: Style.space(10)

            Item {
              width: parent.width
              height: Style.space(48)

              PanelActionButton {
                id: conversationBack
                anchors.left: parent.left
                anchors.verticalCenter: parent.verticalCenter
                iconText: "󰁍"
                tooltipText: "Back to friends"
                foreground: root.foreground
                fontFamily: root.fontFamily
                focusable: true
                Accessible.name: tooltipText
                onClicked: root.closeConversation()
              }

              AvatarButton {
                id: conversationAvatar
                anchors.left: conversationBack.right
                anchors.leftMargin: Style.space(8)
                anchors.verticalCenter: parent.verticalCenter
                label: root.avatarInitial(root.activeFriend)
                tooltipText: root.displayName(root.activeFriend)
                foreground: root.foreground
                interactive: false
              }

              Column {
                anchors.left: conversationAvatar.right
                anchors.leftMargin: Style.space(10)
                anchors.right: conversationMore.left
                anchors.rightMargin: Style.space(8)
                anchors.verticalCenter: parent.verticalCenter
                spacing: Style.space(1)
                Text { width: parent.width; text: root.displayName(root.activeFriend); textFormat: Text.PlainText; elide: Text.ElideRight; color: root.foreground; font.family: root.fontFamily; font.pixelSize: Style.font.body; font.bold: true }
                Text {
                  width: parent.width
                  text: root.activeIncomingPoke ? "poked you" : (root.activeWaitingForReply ? "waiting" : "@" + root.handle(root.activeFriend))
                  textFormat: Text.PlainText
                  elide: Text.ElideRight
                  color: root.activeIncomingPoke ? (root.bar ? root.bar.urgent : Color.urgent) : Qt.darker(root.foreground, 1.45)
                  font.family: root.fontFamily
                  font.pixelSize: Style.font.caption
                }
              }

              PanelActionButton {
                id: conversationMore
                anchors.right: parent.right
                anchors.verticalCenter: parent.verticalCenter
                iconText: "󰇙"
                foreground: root.foreground
                tooltipText: "More actions for @" + root.handle(root.activeFriend)
                focusable: true
                Accessible.name: tooltipText
                onClicked: conversationMenu.opened ? conversationMenu.close() : conversationMenu.open()

                QQC.Popup {
                  id: conversationMenu
                  x: conversationMore.width - width
                  y: conversationMore.height + Style.space(4)
                  width: Style.space(160)
                  padding: Style.space(4)
                  focus: true
                  closePolicy: QQC.Popup.CloseOnEscape | QQC.Popup.CloseOnPressOutsideParent

                  background: BorderSurface {
                    color: Color.popups.background
                    borderSpec: Border.localOrSurfaceSpec("popups", "border", Color.popups.border, Color.popups.border, Math.max(1, Style.normalBorderWidth))
                    radius: Style.cornerRadius
                  }

                  contentItem: Column {
                    spacing: Style.space(2)
                    Button {
                      visible: !!root.activeIncomingPoke
                      width: parent.width
                      text: "Dismiss poke"
                      iconText: "󰅖"
                      leftAlign: true
                      focusable: true
                      foreground: root.foreground
                      fontFamily: root.fontFamily
                      onClicked: { conversationMenu.close(); root.runAction("Dismissing poke…", ["dismiss", String(root.activeIncomingPoke.id)]) }
                    }
                    Button {
                      width: parent.width
                      text: "Remove friend"
                      iconText: "󰆴"
                      leftAlign: true
                      focusable: true
                      foreground: root.foreground
                      fontFamily: root.fontFamily
                      onClicked: { conversationMenu.close(); root.runAction("Removing friend…", ["friends", "remove", "@" + root.handle(root.activeFriend)]) }
                    }
                    Button {
                      width: parent.width
                      text: "Block"
                      iconText: "󰅖"
                      leftAlign: true
                      focusable: true
                      foreground: root.bar ? root.bar.urgent : Color.urgent
                      fontFamily: root.fontFamily
                      onClicked: { conversationMenu.close(); root.runAction("Blocking…", ["block", "@" + root.handle(root.activeFriend)]) }
                    }
                  }
                }
              }
            }

            PanelSeparator { width: parent.width; foreground: root.foreground }

            Text {
              visible: root.activePokeHistory.length === 0
              width: parent.width
              text: "No pokes yet. Send the first one."
              horizontalAlignment: Text.AlignHCenter
              textFormat: Text.PlainText
              color: Qt.darker(root.foreground, 1.45)
              font.family: root.fontFamily
              font.pixelSize: Style.font.body
            }

            Repeater {
              model: root.activePokeHistory
              delegate: Item {
                id: pokeEvent
                required property var modelData
                readonly property bool outgoing: Number(modelData.outgoing) === 1
                width: conversationView.width
                height: pokeBubble.implicitHeight + Style.space(4)

                BorderSurface {
                  id: pokeBubble
                  x: pokeEvent.outgoing ? parent.width - width : 0
                  width: Math.min(parent.width * 0.7, Style.space(250))
                  implicitHeight: pokeBubbleContent.implicitHeight + Style.space(14)
                  color: pokeEvent.outgoing
                    ? Style.selectedFillFor(root.foreground, root.foreground)
                    : Qt.rgba(root.foreground.r, root.foreground.g, root.foreground.b, 0.07)
                  borderSpec: Border.none()
                  radius: Style.cornerRadius

                  Column {
                    id: pokeBubbleContent
                    anchors.left: parent.left
                    anchors.right: parent.right
                    anchors.verticalCenter: parent.verticalCenter
                    anchors.leftMargin: Style.space(9)
                    anchors.rightMargin: Style.space(9)
                    spacing: Style.space(3)
                    Text { width: parent.width; text: pokeEvent.outgoing ? "You poked" : root.displayName(root.activeFriend) + " poked you"; textFormat: Text.PlainText; wrapMode: Text.Wrap; color: root.foreground; font.family: root.fontFamily; font.pixelSize: Style.font.body }
                    Text { width: parent.width; text: root.formatPokeTime(pokeEvent.modelData.created_at); textFormat: Text.PlainText; horizontalAlignment: Text.AlignRight; color: Qt.darker(root.foreground, 1.5); font.family: root.fontFamily; font.pixelSize: Style.font.caption }
                  }
                }
              }
            }

            Button {
              width: parent.width
              text: root.activeSendingPoke ? "Sending…" : (root.activeWaitingForReply ? "Poke sent" : "Poke")
              tooltipText: root.activeWaitingForReply ? "Waiting for @" + root.handle(root.activeFriend) : "Poke @" + root.handle(root.activeFriend)
              foreground: root.activeIncomingPoke ? (root.bar ? root.bar.urgent : Color.urgent) : root.foreground
              fontFamily: root.fontFamily
              focusable: true
              bordered: true
              enabled: !root.activeWaitingForReply && !root.actionRunning
              onClicked: root.runAction("Sending poke…", ["poke", "@" + root.handle(root.activeFriend)])
            }
          }
        }
      }
    }
  }

  Component {
    id: friendRow
    Item {
      id: friendItem
      required property var modelData
      readonly property string friendHandle: root.handle(modelData)
      readonly property var incomingPoke: root.inboxFor(modelData)
      readonly property bool hasIncoming: incomingPoke !== null
      readonly property bool waitingForReply: Number(modelData.waiting) === 1
      readonly property bool sendingPoke: root.actionRunning && root.actionKey === "poke:@" + friendHandle
      width: content.width
      height: Style.space(48)

      BorderSurface {
        id: openFriend
        anchors.left: parent.left
        anchors.right: rowActions.left
        anchors.rightMargin: Style.space(8)
        anchors.top: parent.top
        anchors.bottom: parent.bottom
        radius: Style.cornerRadius
        activeFocusOnTab: true
        color: activeFocus ? Style.focusFillFor(root.foreground, root.foreground)
          : (openFriendMouse.containsMouse ? Style.hoverFillFor(root.foreground, root.foreground) : "transparent")
        borderSpec: activeFocus ? Border.controlSpec("focus", root.foreground, root.foreground) : Border.none()
        Keys.onReturnPressed: root.openConversation(friendItem.modelData)
        Keys.onEnterPressed: root.openConversation(friendItem.modelData)
        Keys.onSpacePressed: root.openConversation(friendItem.modelData)
        Accessible.role: Accessible.Button
        Accessible.name: "Open poke history with @" + friendItem.friendHandle
        Accessible.onPressAction: root.openConversation(friendItem.modelData)

        AvatarButton {
          id: friendAvatar
          anchors.left: parent.left
          anchors.leftMargin: Style.space(4)
          anchors.verticalCenter: parent.verticalCenter
          label: root.avatarInitial(friendItem.modelData)
          tooltipText: ""
          foreground: root.foreground
          interactive: false
        }

        Column {
          anchors.left: friendAvatar.right
          anchors.leftMargin: Style.space(10)
          anchors.right: parent.right
          anchors.rightMargin: Style.space(8)
          anchors.verticalCenter: parent.verticalCenter
          spacing: Style.space(1)
          Text { width: parent.width; text: root.displayName(friendItem.modelData); textFormat: Text.PlainText; elide: Text.ElideRight; color: root.foreground; font.family: root.fontFamily; font.pixelSize: Style.font.body }
          Text { visible: friendItem.hasIncoming || friendItem.waitingForReply; width: parent.width; text: friendItem.hasIncoming ? "poked you" : "waiting"; textFormat: Text.PlainText; elide: Text.ElideRight; color: friendItem.hasIncoming ? (root.bar ? root.bar.urgent : Color.urgent) : Qt.darker(root.foreground, 1.45); font.family: root.fontFamily; font.pixelSize: Style.font.caption }
        }

        MouseArea {
          id: openFriendMouse
          anchors.fill: parent
          hoverEnabled: true
          cursorShape: Qt.PointingHandCursor
          onClicked: { openFriend.forceActiveFocus(); root.openConversation(friendItem.modelData) }
        }
      }

      Row {
        id: rowActions
        anchors.right: parent.right
        anchors.verticalCenter: parent.verticalCenter
        Button {
          width: Style.space(68)
          text: friendItem.sendingPoke ? "Sending" : (friendItem.waitingForReply ? "Sent" : "Poke")
          tooltipText: friendItem.waitingForReply ? "Waiting for @" + friendItem.friendHandle : (friendItem.hasIncoming ? "Poke @" + friendItem.friendHandle + " back" : "Poke @" + friendItem.friendHandle)
          foreground: friendItem.hasIncoming ? (root.bar ? root.bar.urgent : Color.urgent) : root.foreground
          fontFamily: root.fontFamily
          focusable: true
          bordered: true
          enabled: !friendItem.waitingForReply && !root.actionRunning
          onClicked: root.runAction("Sending poke…", ["poke", "@" + friendItem.friendHandle])
        }
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
