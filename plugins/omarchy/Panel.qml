import QtQuick
import QtQuick.Effects
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
  property var pokachyState: ({
      needsLogin: true
    })
  property bool stateLoaded: false
  property string actionError: ""
  property string actionNotice: ""
  property string statusError: ""
  readonly property string lastError: actionError !== "" ? actionError : statusError
  property string actionLabel: ""
  property string actionKey: ""
  property string activeFriendHandle: ""
  property var olderHistory: []
  property string historyCursor: ""
  property string historyError: ""
  property int historySession: 0
  property int historyRequestSession: -1
  property string historyOutput: ""
  property bool historyLoaded: false
  property bool historyStickToBottom: true
  property bool historyUpdating: false
  property real historyPrependHeight: -1
  property real historyPrependY: 0
  property real historyAnchorY: 0
  property real historyAnchorHeight: -1
  property int historyAnchorSession: -1
  property bool historyAnchorPending: false
  property var retryArgs: []
  property var pendingPokes: ({})
  property double labelNow: Date.now()
  Timer {
    interval: 60000
    running: true
    repeat: true
    onTriggered: root.labelNow = Date.now()
  }
  readonly property bool actionRunning: actionProc.running
  readonly property bool needsLogin: pokachyState && pokachyState.needsLogin === true
  readonly property bool online: pokachyState && pokachyState.online === true
  readonly property color secondaryForeground: Qt.rgba(foreground.r, foreground.g, foreground.b, 0.65)
  readonly property int inboxCount: pokachyState && pokachyState.inbox ? pokachyState.inbox.length : 0
  readonly property color foreground: bar ? bar.foreground : Color.foreground
  readonly property string fontFamily: bar ? bar.fontFamily : Style.font.family
  readonly property var activeFriend: friendForHandle(activeFriendHandle)
  readonly property var activeIncomingPoke: inboxFor(activeFriend)
  readonly property var activePokeHistory: historyFor(activeFriend)
  onActivePokeHistoryChanged: {
    if (!activeFriend)
      return
    historyAnchorHeight = historyPrependHeight
    historyAnchorY = historyAnchorHeight >= 0 ? historyPrependY : historyScroll.contentY
    historyAnchorSession = historySession
    historyPrependHeight = -1
    historyUpdating = true
    historyAnchorPending = true
    historyAnchorTimer.restart()
  }
  readonly property bool activeWaitingForReply: activeFriend && (Number(activeFriend.waiting) === 1 || pendingPokes[handle(activeFriend)] === true)
  // Repeater delegates can finish layout after Qt.callLater. Restart this
  // short settling timer on height changes, then restore the visible anchor.
  Timer {
    id: historyAnchorTimer
    interval: 32
    onTriggered: {
      if (root.historyAnchorSession === root.historySession && root.activeFriend) {
        var desiredY = root.historyAnchorHeight >= 0 ? root.historyAnchorY + historyScroll.contentHeight - root.historyAnchorHeight : root.historyAnchorY
        historyScroll.contentY = root.historyStickToBottom ? Math.max(0, historyScroll.contentHeight - historyScroll.height) : Math.max(0, Math.min(desiredY, Math.max(0, historyScroll.contentHeight - historyScroll.height)))
      }
      root.historyAnchorPending = false
      root.historyUpdating = false
    }
  }
  readonly property bool activeSendingPoke: activeFriend && actionRunning && actionKey === "poke:@" + handle(activeFriend)
  readonly property string barTooltip: needsLogin ? "Pokachy · connect this computer" : (inboxCount > 0 ? "Pokachy · " + inboxCount + " waiting" : "Pokachy · " + (online ? "online" : "offline"))

  component AvatarButton: BorderSurface {
    id: avatar
    property string label: "?"
    property string image: ""
    property string tooltipText: ""
    property color foreground: root.foreground
    property bool interactive: false
    property bool expanded: false
    property bool openHistoryOnHover: false
    signal hoverActivated
    property real size: Style.space(34)
    signal clicked

    implicitWidth: size
    implicitHeight: size
    radius: size / 2
    activeFocusOnTab: interactive
    Keys.onReturnPressed: if (interactive)
      avatar.clicked()
    Keys.onEnterPressed: if (interactive)
      avatar.clicked()
    Keys.onSpacePressed: if (interactive)
      avatar.clicked()
    Accessible.role: interactive ? Accessible.Button : Accessible.StaticText
    Accessible.name: tooltipText
    Accessible.onPressAction: if (interactive)
      avatar.clicked()

    readonly property bool hot: interactive && avatarMouse.containsMouse
    Timer {
      id: hoverHistoryTimer
      interval: 450
      running: avatar.openHistoryOnHover && avatar.hot && avatar.visible && root.opened && !root.activeFriend && !panelScroll.moving && !panelScroll.dragging
      onTriggered: {
        if (avatar.openHistoryOnHover && avatar.hot && avatar.visible && root.opened && !root.activeFriend && !panelScroll.moving && !panelScroll.dragging)
          avatar.hoverActivated()
      }
    }
    color: activeFocus ? Style.focusFillFor(foreground, foreground) : (hot || expanded ? Style.selectedFillFor(foreground, foreground) : Qt.rgba(foreground.r, foreground.g, foreground.b, 0.12))
    borderSpec: activeFocus ? Border.controlSpec("focus", foreground, foreground) : (expanded ? Border.controlSpec("selected", foreground, foreground) : Border.none())

    Text {
      visible: photo.status !== Image.Ready
      anchors.centerIn: parent
      text: avatar.label
      textFormat: Text.PlainText
      color: avatar.foreground
      font.family: root.fontFamily
      font.pixelSize: Style.font.subtitle
      font.bold: true
    }

    Rectangle {
      id: photoMask
      anchors.fill: parent
      radius: width / 2
      color: "white"
      antialiasing: true
      visible: false
      layer.enabled: true
    }
    Image {
      id: photo
      anchors.fill: parent
      source: /^https:\/\/avatars\.githubusercontent\.com\//.test(avatar.image) ? avatar.image : ""
      sourceSize.width: avatar.size
      sourceSize.height: avatar.size
      fillMode: Image.PreserveAspectCrop
      asynchronous: true
      cache: true
      visible: status === Image.Ready
      layer.enabled: true
      layer.smooth: true
      layer.effect: MultiEffect {
        maskEnabled: true
        maskSource: photoMask
      }
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
    if (!person)
      return "Someone"
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
      if (handle(friends[i]) === wanted)
        return friends[i]
    }
    return null
  }
  function normalizedSearch(value) {
    return String(value || "").trim().toLowerCase().replace(/^@/, "")
  }
  function filteredFriends(value) {
    return filteredPeople("friends", value)
  }
  function filteredPeople(kind, value) {
    var query = normalizedSearch(value)
    var friends = list(kind)
    if (query === "")
      return friends
    var matches = []
    for (var i = 0; i < friends.length; i++) {
      var friendHandle = handle(friends[i]).toLowerCase()
      var friendName = String(friends[i].name || "").toLowerCase()
      if (friendHandle.indexOf(query) !== -1 || friendName.indexOf(query) !== -1)
        matches.push(friends[i])
    }
    return matches
  }
  function exactSearchFriend(value) {
    var query = normalizedSearch(value)
    return query === "" ? null : friendForHandle(query)
  }
  function canAddSearch(value) {
    var query = normalizedSearch(value)
    return /^[a-z0-9][a-z0-9_]{2,23}$/.test(query) && filteredFriends(value).length === 0 && filteredPeople("requests", value).length === 0 && filteredPeople("blocked", value).length === 0
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
    if (canAddSearch(friendSearch.text))
      addFriend(friendSearch.text)
  }
  function inboxFor(person) {
    var friendHandle = handle(person)
    var inbox = list("inbox")
    for (var i = 0; i < inbox.length; i++) {
      if (handle(inbox[i]) === friendHandle)
        return inbox[i]
    }
    return null
  }
  function historyFor(person) {
    var friendHandle = handle(person)
    var history = list("history").concat(olderHistory)
    var matches = []
    var seen = ({})
    for (var i = 0; i < history.length; i++) {
      if (handle(history[i]) === friendHandle && !seen[history[i].id]) {
        seen[history[i].id] = true
        matches.push(history[i])
      }
    }
    return matches.sort(function (a, b) {
      return Number(a.created_at) - Number(b.created_at) || (String(a.id) < String(b.id) ? -1 : (String(a.id) > String(b.id) ? 1 : 0))
    })
  }
  function avatarInitial(person) {
    var source = String(person && (person.handle || person.name) || "?").trim().replace(/^@/, "")
    return source === "" ? "?" : source.charAt(0).toUpperCase()
  }
  // State history is capped globally; never infer an event from waiting/presence.
  function latestKnownPoke(person) {
    var events = list("history").concat(list("inbox"), olderHistory)
    var latest = 0
    for (var i = 0; i < events.length; i++) {
      var time = Number(events[i].created_at)
      if (handle(events[i]) === handle(person) && isFinite(time) && time > 0 && time <= labelNow && isFinite(new Date(time).getTime()))
        latest = Math.max(latest, time)
    }
    return latest
  }
  function relativePokeTime(time, now) {
    if (!isFinite(time) || time <= 0 || time > now || !isFinite(new Date(time).getTime()))
      return ""
    var minutes = Math.floor((now - time) / 60000)
    if (minutes < 1) return "Now"
    if (minutes < 60) return minutes + "m"
    var date = new Date(time)
    var today = new Date(now)
    if (date.toDateString() === today.toDateString()) return Math.floor(minutes / 60) + "h"
    today.setDate(today.getDate() - 1)
    if (date.toDateString() === today.toDateString()) return "Yesterday"
    return Qt.formatDateTime(date, date.getFullYear() === new Date(now).getFullYear() ? "MMM d" : "MMM d, yyyy")
  }
  function pokeTimeTooltip(time) {
    return "Latest known poke · " + Qt.formatDateTime(new Date(time), "MMM d, yyyy HH:mm:ss t") + " · either direction; cached history may be incomplete"
  }
  function formatPokeTime(value) {
    var milliseconds = Number(value)
    if (!isFinite(milliseconds) || milliseconds <= 0)
      return ""
    return Qt.formatDateTime(new Date(milliseconds), "HH:mm")
  }
  function pokeDate(value) {
    var date = new Date(Number(value))
    if (!isFinite(date.getTime()))
      return ""
    var today = Qt.formatDateTime(new Date(), "yyyy-MM-dd")
    return Qt.formatDateTime(date, "yyyy-MM-dd") === today ? "Today" : Qt.formatDateTime(date, "MMM d, yyyy")
  }
  function loadHistory() {
    if (!activeFriend || historyProc.running || (historyLoaded && historyCursor === ""))
      return
    historyRequestSession = historySession
    historyError = ""
    historyOutput = ""
    var args = ["/usr/bin/env", "pokachy", "history", "@" + activeFriendHandle, "--json"]
    if (historyLoaded && historyCursor !== "")
      args = args.concat(["--before", historyCursor])
    historyProc.command = args
    historyProc.running = true
  }
  function openConversation(person) {
    historySession++
    historyStickToBottom = true
    activeFriendHandle = handle(person)
    olderHistory = []
    historyCursor = ""
    historyError = ""
    historyLoaded = false
    historyStickToBottom = true
    friendSearch.text = ""
    panelScroll.contentY = 0
    Qt.callLater(function () {
      historyScroll.contentY = Math.max(0, historyScroll.contentHeight - historyScroll.height)
      root.loadHistory()
      conversationBack.forceActiveFocus()
    })
  }
  function closeConversation() {
    historySession++
    activeFriendHandle = ""
    conversationMenu.close()
    panelScroll.contentY = 0
    Qt.callLater(function () {
      friendSearch.forceActiveFocus()
    })
  }
  function ensureListItemVisible(item) {
    if (activeFriend || !item)
      return
    var top = item.mapToItem(content, 0, 0).y
    if (top < panelScroll.contentY)
      panelScroll.contentY = top
    else if (top + item.height > panelScroll.contentY + panelScroll.height)
      panelScroll.contentY = top + item.height - panelScroll.height
  }
  function parseState(line) {
    try {
      var next = JSON.parse(String(line))
      if (next && typeof next === "object" && (next.needsLogin !== undefined || (next.me && typeof next.me === "object"))) {
        labelNow = Date.now()
        pokachyState = next
        stateLoaded = true
        var pending = Object.assign({}, pendingPokes)
        var confirmed = (next.friends || []).concat(next.inbox || [])
        for (var i = 0; i < confirmed.length; i++) {
          if (Number(confirmed[i].waiting) === 1 || (next.inbox || []).indexOf(confirmed[i]) !== -1)
            delete pending[handle(confirmed[i])]
        }
        pendingPokes = pending
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
    if (actionProc.running)
      return
    actionError = ""
    actionNotice = ""
    actionNoticeTimer.stop()
    actionLabel = label
    actionKey = args.join(":")
    retryArgs = args.slice()
    actionProc.command = ["/usr/bin/env", "pokachy"].concat(args)
    actionProc.running = true
  }
  function addFriend(value) {
    var friendHandle = normalizedSearch(value)
    if (!canAddSearch(friendHandle))
      return
    runAction("Adding friend…", ["friends", "add", "@" + friendHandle])
    friendSearch.text = ""
  }
  function setup() {
    // This command is fully static. No state, token, or user supplied input
    // is passed through the shell launcher.
    if (bar)
      bar.run("omarchy-launch-floating-terminal-with-presentation pokachy init")
  }
  function open() {
    refresh()
    controller.show()
  }
  function close() {
    closeConversation()
    friendSearch.text = ""
    controller.hide()
  }
  function toggle() {
    opened ? close() : open()
  }
  function closeForPopoutSwitch() {
    root.popoutSwitchClosing = true
    close()
    Qt.callLater(function () {
      root.popoutSwitchClosing = false
    })
  }
  function switchPanel(direction) {
    return bar && typeof bar.switchPanelFrom === "function" ? bar.switchPanelFrom(barIdentity, direction) : false
  }

  Process {
    id: watchProc
    running: true
    command: ["/usr/bin/env", "pokachy", "watch", "--json"]
    stdout: SplitParser {
      onRead: function (line) {
        root.parseState(line)
      }
    }
    stderr: SplitParser {
      onRead: function (line) {
        root.statusError = "Pokachy is unavailable. Check that the CLI is installed."
      }
    }
    onExited: function (code) {
      if (code !== 0 && root.statusError === "")
        root.statusError = "Pokachy is unavailable. Check that the CLI is installed."
      if (code !== 0)
        watchRestart.restart()
    }
  }
  Timer {
    id: watchRestart
    interval: 3000
    onTriggered: if (!watchProc.running)
      watchProc.running = true
  }
  Process {
    id: statusProc
    command: []
    stdout: SplitParser {
      onRead: function (line) {
        root.parseState(line)
      }
    }
    stderr: SplitParser {
      onRead: function (line) {
        root.statusError = "Pokachy is unavailable. Check that the CLI is installed."
      }
    }
    onExited: function (code) {
      if (code !== 0 && root.statusError === "")
        root.statusError = "Could not refresh Pokachy."
    }
  }
  Process {
    id: historyProc
    stdout: StdioCollector {
      onStreamFinished: root.historyOutput = text
    }
    stderr: StdioCollector {
      onStreamFinished: if (root.historyRequestSession === root.historySession)
        root.historyError = text.trim()
    }
    onExited: function (code) {
      if (root.historyRequestSession !== root.historySession) {
        Qt.callLater(root.loadHistory)
        return
      }
      if (code !== 0) {
        if (root.historyError === "")
          root.historyError = "Could not load older pokes."
        return
      }
      try {
        var page = JSON.parse(root.historyOutput)
        if (!(page.history instanceof Array))
          throw new Error("Invalid history")
        // Capture the current viewport, not where it was when the request
        // started: the user may have kept scrolling while the page loaded.
        root.historyPrependHeight = historyScroll.contentHeight
        root.historyPrependY = historyScroll.contentY
        root.olderHistory = root.olderHistory.concat(page.history)
        root.historyCursor = page.next_cursor || ""
        root.historyLoaded = true
      } catch (error) {
        root.historyError = "Could not read poke history."
      }
    }
  }
  Process {
    id: actionProc
    command: []
    // Action commands write human confirmation such as "Done.", not State
    // JSON. The status command and the watcher own state updates.
    stdout: SplitParser {
      onRead: function (line) {
        var message = String(line).trim()
        if (message !== "" && root.retryArgs[0] !== "poke") {
          root.actionNotice = message
          actionNoticeTimer.restart()
        }
      }
    }
    stderr: SplitParser {
      onRead: function (line) {
        root.actionError = String(line).trim().slice(0, 300)
      }
    }
    onExited: function (code) {
      if (code !== 0 && root.actionError === "")
        root.actionError = root.actionLabel + " failed."
      if (code === 0 && root.actionNotice === "" && root.retryArgs[0] !== "poke") {
        root.actionNotice = "Done."
        actionNoticeTimer.restart()
      }
      if (code === 0 && root.retryArgs[0] === "poke") {
        var pending = Object.assign({}, root.pendingPokes)
        pending[String(root.retryArgs[1]).replace(/^@/, "")] = true
        root.pendingPokes = pending
        pendingTimer.restart()
      }
      root.actionLabel = ""
      root.refresh()
    }
  }
  Timer {
    id: actionNoticeTimer
    interval: 3500
    onTriggered: root.actionNotice = ""
  }
  Timer {
    id: pendingTimer
    interval: 10000
    onTriggered: {
      root.pendingPokes = ({})
      root.refresh()
    }
  }

  IpcHandler {
    target: root.ipcTarget
    function open(): void {
      root.open()
    }
    function close(): void {
      root.close()
    }
    function toggle(): void {
      root.toggle()
    }
    function refresh(): void {
      root.refresh()
    }
  }

  SquareKeyboardPanel {
    id: panel
    anchorItem: root.anchorItem
    owner: root.barIdentity
    bar: root.bar
    open: root.opened
    focusTarget: keyCatcher
    contentWidth: panel.fittedContentWidth(Style.space(430), Style.space(520))
    contentHeight: panel.fittedContentHeight(root.activeFriend ? Style.space(560) : content.implicitHeight, Style.space(650))

    PanelKeyCatcher {
      id: keyCatcher
      anchors.fill: parent
      // This panel contains standard focusable controls, so leave their key
      // events alone and let Qt's native Tab chain drive navigation.
      blocked: true
      onCloseRequested: root.close()

      Shortcut {
        sequence: "Escape"
        enabled: root.opened
        onActivated: conversationMenu.opened ? conversationMenu.close() : (root.activeFriend ? root.closeConversation() : root.close())
      }

      Flickable {
        id: panelScroll
        anchors.fill: parent
        contentWidth: width
        contentHeight: root.activeFriend ? height : content.implicitHeight
        clip: true
        boundsBehavior: Flickable.StopAtBounds
        interactive: !root.activeFriend && contentHeight > height
        QQC.ScrollBar.vertical: QQC.ScrollBar {
          policy: QQC.ScrollBar.AsNeeded
        }

        Column {
          id: content
          width: parent.width
          height: root.activeFriend ? panelScroll.height : implicitHeight
          spacing: Style.space(12)

          PanelHero {
            visible: root.needsLogin
            width: parent.width
            foreground: root.foreground
            fontFamily: root.fontFamily
            title: !root.stateLoaded ? (root.lastError !== "" ? "Pokachy unavailable" : "Loading Pokachy…") : (root.needsLogin ? "Connect Pokachy" : (root.pokachyState.me && root.pokachyState.me.handle ? "@" + root.pokachyState.me.handle : "Pokachy"))
            meta: root.needsLogin ? "A little nudge for your Linux friends" : (root.online ? "" : "Offline · cached activity")
            detail: root.needsLogin ? "SETUP" : (root.inboxCount > 0 ? root.inboxCount + (root.inboxCount === 1 ? " POKE" : " POKES") : "")
            iconComponent: Component {
              AvatarButton {
                label: root.needsLogin ? "" : root.avatarInitial(root.pokachyState.me)
                image: root.needsLogin ? "" : String(root.pokachyState.me && root.pokachyState.me.image || "")
                tooltipText: root.needsLogin ? "Pokachy" : "Your profile"
                foreground: root.foreground
                interactive: false
                size: Style.space(36)
                opacity: root.needsLogin ? 0.55 : 1.0
                BrandIcon {
                  visible: root.needsLogin
                  anchors.centerIn: parent
                  width: Style.space(32)
                  height: width
                  foreground: root.foreground
                }
              }
            }
            trailingControl: root.needsLogin ? null : accountActions
          }

          Item {
            visible: !root.needsLogin && !root.activeFriend
            width: parent.width
            height: Style.space(44)
            AvatarButton {
              id: ownerAvatar
              anchors.left: parent.left
              anchors.verticalCenter: parent.verticalCenter
              label: root.avatarInitial(root.pokachyState.me)
              image: String(root.pokachyState.me && root.pokachyState.me.image || "")
              size: Style.space(36)
              Rectangle {
                anchors.right: parent.right
                anchors.top: parent.top
                width: Style.space(10)
                height: width
                radius: width / 2
                color: root.online ? "#75b56b" : Color.muted
                border.width: Style.space(2)
                border.color: Color.popups.background
                Accessible.role: Accessible.StaticText
                Accessible.name: root.online ? "Connected to Pokachy" : "Offline · cached activity"
                MouseArea { id: connectionMouse; anchors.fill: parent; hoverEnabled: true }
                PanelToolTip {
                  visible: connectionMouse.containsMouse
                  text: root.online ? "Connected to Pokachy" : "Offline · cached activity"
                  fontFamily: root.fontFamily
                }
              }
            }
            Column {
              anchors.left: ownerAvatar.right
              anchors.leftMargin: Style.space(12)
              anchors.right: quietControl.left
              anchors.rightMargin: Style.space(8)
              anchors.verticalCenter: parent.verticalCenter
              spacing: Style.space(2)
              Text {
                width: parent.width
                text: root.displayName(root.pokachyState.me)
                textFormat: Text.PlainText
                elide: Text.ElideRight
                color: root.foreground
                font.family: root.fontFamily
                font.pixelSize: Style.font.body
                font.bold: true
              }
              Text {
                width: parent.width
                text: root.handle(root.pokachyState.me) ? "@" + root.handle(root.pokachyState.me) : ""
                textFormat: Text.PlainText
                elide: Text.ElideRight
                color: root.secondaryForeground
                font.family: root.fontFamily
                font.pixelSize: Style.font.caption
              }
            }
            Loader {
              id: quietControl
              anchors.right: parent.right
              anchors.verticalCenter: parent.verticalCenter
              sourceComponent: accountActions
            }
          }

          Component {
            id: accountActions
            PanelActionButton {
              iconText: root.pokachyState.me && root.pokachyState.me.quiet ? "󰂛" : "󰂚"
              tooltipText: root.pokachyState.me && root.pokachyState.me.quiet ? "Resume notifications" : "Quiet notifications"
              foreground: root.foreground
              fontFamily: root.fontFamily
              size: Style.space(36)
              fontSize: Style.space(20)
              focusable: true
              Accessible.name: tooltipText
              onClicked: root.runAction("Updating quiet mode…", ["quiet", root.pokachyState.me && root.pokachyState.me.quiet ? "off" : "on"])
            }
          }

          Rectangle {
            visible: root.lastError !== ""
            width: parent.width
            implicitHeight: errorText.implicitHeight + Style.space(16) + (retryButton.visible ? retryButton.height + Style.space(8) : 0)
            radius: Style.cornerRadius
            color: Style.hoverFillFor(root.bar ? root.bar.urgent : Color.urgent, root.bar ? root.bar.urgent : Color.urgent)
            Text {
              id: errorText
              anchors.top: parent.top
              anchors.left: parent.left
              anchors.right: parent.right
              anchors.margins: Style.space(8)
              text: root.lastError
              textFormat: Text.PlainText
              wrapMode: Text.Wrap
              color: root.foreground
              font.family: root.fontFamily
              font.pixelSize: Style.font.body
            }
            Button {
              id: retryButton
              anchors.top: errorText.bottom
              anchors.topMargin: Style.space(8)
              anchors.left: parent.left
              anchors.leftMargin: Style.space(8)
              visible: root.actionError !== "" && root.retryArgs.length > 0
              text: "Try again"
              focusable: true
              enabled: !root.actionRunning
              onClicked: root.runAction("Trying again…", root.retryArgs)
            }
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
            visible: root.needsLogin && root.stateLoaded
            width: parent.width
            spacing: Style.space(10)
            Text {
              width: parent.width
              text: "Set up this computer in a terminal, approve the device code in your browser, then Pokachy will appear here."
              wrapMode: Text.Wrap
              color: Qt.darker(root.foreground, 1.35)
              font.family: root.fontFamily
              font.pixelSize: Style.font.body
            }
            Button {
              text: "Open setup"
              focusable: true
              onClicked: root.setup()
            }
          }

          Column {
            id: friendListView
            visible: !root.needsLogin && !root.activeFriend
            width: parent.width
            spacing: Style.space(8)

            TextField {
              id: friendSearch
              width: parent.width
              placeholderText: "Search"
              background: BorderSurface {
                radius: 0
                color: Style.controlFill(friendSearch.activeFocus, friendSearch.hovered, root.foreground, Color.accent)
                borderSpec: Border.controlSpec(friendSearch.activeFocus ? "focus" : (friendSearch.hovered ? "hover-cursor" : "normal"), root.foreground, Color.accent)
              }
              activeFocusOnTab: true
              Accessible.name: "Find a contact or add a handle"
              onAccepted: root.submitFriendSearch()
            }
            Repeater {
              model: root.filteredFriends(friendSearch.text)
              delegate: friendRow
            }
            Button {
              visible: root.canAddSearch(friendSearch.text)
              width: parent.width
              text: "Add @" + root.normalizedSearch(friendSearch.text)
              iconText: "󰐕"
              leftAlign: true
              focusable: true
              enabled: !root.actionRunning
              foreground: root.foreground
              fontFamily: root.fontFamily
              onClicked: root.addFriend(friendSearch.text)
            }
            Text {
              visible: friendSearch.text.trim() !== "" && root.filteredFriends(friendSearch.text).length === 0 && root.filteredPeople("requests", friendSearch.text).length === 0 && root.filteredPeople("blocked", friendSearch.text).length === 0 && !root.canAddSearch(friendSearch.text)
              width: parent.width
              text: "No matching contact"
              horizontalAlignment: Text.AlignHCenter
              color: Qt.darker(root.foreground, 1.45)
              font.family: root.fontFamily
              font.pixelSize: Style.font.body
            }
            Text {
              visible: friendSearch.text.trim() === "" && root.list("friends").length === 0
              text: "Search for a handle to add your first friend."
              color: Qt.darker(root.foreground, 1.45)
              font.family: root.fontFamily
              font.pixelSize: Style.font.body
            }

            PanelSeparator {
              visible: root.filteredPeople("requests", friendSearch.text).length > 0
              foreground: root.foreground
            }
            PanelSectionHeader {
              visible: root.filteredPeople("requests", friendSearch.text).length > 0
              text: "Requests"
              foreground: root.foreground
              fontFamily: root.fontFamily
            }
            Repeater {
              model: root.filteredPeople("requests", friendSearch.text)
              delegate: requestRow
            }

            PanelSeparator {
              visible: root.filteredPeople("blocked", friendSearch.text).length > 0
              foreground: root.foreground
            }
            PanelSectionHeader {
              visible: root.filteredPeople("blocked", friendSearch.text).length > 0
              text: "Blocked"
              foreground: root.foreground
              fontFamily: root.fontFamily
            }
            Repeater {
              model: root.filteredPeople("blocked", friendSearch.text)
              delegate: blockedRow
            }
          }

          Column {
            id: conversationView
            visible: !root.needsLogin && !!root.activeFriend
            width: parent.width
            height: Math.max(Style.space(160), content.height - y)
            spacing: Style.space(10)

            Item {
              id: conversationHeader
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
                image: String(root.activeFriend && root.activeFriend.image || "")
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
                Text {
                  width: parent.width
                  text: root.displayName(root.activeFriend)
                  textFormat: Text.PlainText
                  elide: Text.ElideRight
                  color: root.foreground
                  font.family: root.fontFamily
                  font.pixelSize: Style.font.body
                  font.bold: true
                }
                Text {
                  width: parent.width
                  text: root.activeIncomingPoke ? "poked you" : (root.activeWaitingForReply ? "waiting for poke back" : "@" + root.handle(root.activeFriend))
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
                      onClicked: {
                        conversationMenu.close()
                        root.runAction("Dismissing poke…", ["dismiss", String(root.activeIncomingPoke.id)])
                      }
                    }
                    Button {
                      width: parent.width
                      text: "Remove friend"
                      iconText: "󰆴"
                      leftAlign: true
                      focusable: true
                      foreground: root.foreground
                      fontFamily: root.fontFamily
                      onClicked: {
                        conversationMenu.close()
                        root.runAction("Removing friend…", ["friends", "remove", "@" + root.handle(root.activeFriend)])
                      }
                    }
                    Button {
                      width: parent.width
                      text: "Block"
                      iconText: "󰅖"
                      leftAlign: true
                      focusable: true
                      foreground: root.bar ? root.bar.urgent : Color.urgent
                      fontFamily: root.fontFamily
                      onClicked: {
                        conversationMenu.close()
                        root.runAction("Blocking…", ["block", "@" + root.handle(root.activeFriend)])
                      }
                    }
                  }
                }
              }
            }

            PanelSeparator {
              id: conversationRule
              width: parent.width
              foreground: root.foreground
            }

            Flickable {
              id: historyScroll
              width: parent.width
              height: Math.max(Style.space(50), conversationView.height - conversationHeader.height - conversationRule.height - conversationPoke.height - conversationView.spacing * 3)
              contentWidth: width
              contentHeight: historyContent.implicitHeight
              clip: true
              boundsBehavior: Flickable.StopAtBounds
              QQC.ScrollBar.vertical: QQC.ScrollBar {
                policy: QQC.ScrollBar.AsNeeded
              }
              onMovementStarted: {
                root.historyStickToBottom = false
                root.historyAnchorPending = false
                root.historyUpdating = false
                historyAnchorTimer.stop()
              }
              onContentYChanged: if (!root.historyUpdating && (moving || dragging))
                root.historyStickToBottom = contentY >= contentHeight - height - Style.space(12)
              onMovementEnded: {
                if (!root.historyUpdating)
                  root.historyStickToBottom = contentY >= contentHeight - height - Style.space(12)
                if (contentY <= Style.space(8) && root.historyLoaded && root.historyCursor !== "")
                  root.loadHistory()
              }
              onContentHeightChanged: {
                if (root.historyAnchorPending)
                  historyAnchorTimer.restart()
                else
                  Qt.callLater(function () {
                    if (root.historyStickToBottom && !root.historyUpdating)
                      historyScroll.contentY = Math.max(0, historyScroll.contentHeight - historyScroll.height)
                  })
              }
              Column {
                id: historyContent
                width: historyScroll.width
                spacing: Style.space(8)
                Button {
                  visible: !root.historyLoaded || root.historyCursor !== "" || root.historyError !== ""
                  width: parent.width
                  text: historyProc.running ? "Loading pokes…" : (root.historyError !== "" ? "Retry loading history" : (root.historyLoaded ? "Older pokes" : "Load history"))
                  enabled: !historyProc.running
                  focusable: true
                  onClicked: {
                    root.historyStickToBottom = false
                    root.loadHistory()
                  }
                }
                Text {
                  visible: root.historyError !== ""
                  width: parent.width
                  text: root.historyError
                  textFormat: Text.PlainText
                  wrapMode: Text.Wrap
                  color: root.foreground
                  font.family: root.fontFamily
                  font.pixelSize: Style.font.caption
                }
                Text {
                  visible: root.activePokeHistory.length === 0
                  width: parent.width
                  text: historyProc.running ? "Loading pokes…" : (root.historyError !== "" ? "No cached pokes for this contact." : "No pokes yet. Send the first one.")
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
                    required property int index
                    readonly property bool outgoing: Number(modelData.outgoing) === 1
                    readonly property string dateLabel: root.pokeDate(modelData.created_at)
                    readonly property bool startsDate: index === 0 || dateLabel !== root.pokeDate(root.activePokeHistory[index - 1].created_at)
                    width: historyScroll.width
                    height: pokeBubble.implicitHeight + Style.space(4) + (startsDate ? Style.space(30) : 0)
                    Text {
                      visible: pokeEvent.startsDate
                      width: parent.width
                      text: pokeEvent.dateLabel
                      horizontalAlignment: Text.AlignHCenter
                      color: Qt.rgba(root.foreground.r, root.foreground.g, root.foreground.b, 0.65)
                      font.family: root.fontFamily
                      font.pixelSize: Style.font.caption
                    }

                    BorderSurface {
                      id: pokeBubble
                      x: pokeEvent.outgoing ? parent.width - width : 0
                      y: pokeEvent.startsDate ? Style.space(30) : 0
                      width: Math.min(parent.width * 0.7, Style.space(250))
                      implicitHeight: pokeBubbleContent.implicitHeight + Style.space(14)
                      color: pokeEvent.outgoing ? Style.selectedFillFor(root.foreground, root.foreground) : Qt.rgba(root.foreground.r, root.foreground.g, root.foreground.b, 0.07)
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
                        Text {
                          width: parent.width
                          text: pokeEvent.outgoing ? "You poked" : root.displayName(root.activeFriend) + " poked you"
                          textFormat: Text.PlainText
                          wrapMode: Text.Wrap
                          color: root.foreground
                          font.family: root.fontFamily
                          font.pixelSize: Style.font.body
                        }
                        Text {
                          width: parent.width
                          text: root.formatPokeTime(pokeEvent.modelData.created_at)
                          textFormat: Text.PlainText
                          horizontalAlignment: Text.AlignRight
                          color: root.secondaryForeground
                          font.family: root.fontFamily
                          font.pixelSize: Style.font.caption
                        }
                      }
                    }
                  }
                }
              }
            }

            PokeButton {
              id: conversationPoke
              focusFallback: conversationBack
              fontFamily: root.fontFamily
              foreground: root.activeIncomingPoke ? Color.accent : root.foreground
              width: parent.width
              height: Style.space(38)
              sending: root.activeSendingPoke
              incoming: !!root.activeIncomingPoke
              waiting: root.activeWaitingForReply
              busy: root.actionRunning
              contactHandle: root.handle(root.activeFriend)
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
      readonly property bool waitingForReply: Number(modelData.waiting) === 1 || root.pendingPokes[friendHandle] === true
      readonly property bool sendingPoke: root.actionRunning && root.actionKey === "poke:@" + friendHandle
      width: content.width
      height: Style.space(48)

      Item {
        id: friendIdentity
        anchors.left: parent.left
        anchors.right: rowActions.left
        anchors.rightMargin: Style.space(8)
        anchors.top: parent.top
        anchors.bottom: parent.bottom

        AvatarButton {
          id: friendAvatar
          anchors.left: parent.left
          anchors.leftMargin: Style.space(4)
          anchors.verticalCenter: parent.verticalCenter
          label: root.avatarInitial(friendItem.modelData)
          image: String(friendItem.modelData.image || "")
          tooltipText: "Open poke history with @" + friendItem.friendHandle
          foreground: root.foreground
          interactive: true
          size: Style.space(36)
          openHistoryOnHover: true
          onHoverActivated: root.openConversation(friendItem.modelData)
          onClicked: root.openConversation(friendItem.modelData)
          onActiveFocusChanged: if (activeFocus)
            root.ensureListItemVisible(friendItem)
        }

        Column {
          anchors.left: friendAvatar.right
          anchors.leftMargin: Style.space(10)
          anchors.right: parent.right
          anchors.rightMargin: Style.space(8)
          anchors.verticalCenter: parent.verticalCenter
          spacing: Style.space(1)
          Text {
            width: parent.width
            text: root.displayName(friendItem.modelData)
            font.bold: true
            textFormat: Text.PlainText
            elide: Text.ElideRight
            color: root.foreground
            font.family: root.fontFamily
            font.pixelSize: Style.font.body
          }
          Text {
            width: parent.width
            text: "@" + friendItem.friendHandle
            textFormat: Text.PlainText
            elide: Text.ElideRight
            color: root.secondaryForeground
            font.family: root.fontFamily
            font.pixelSize: Style.font.caption
          }
        }


      }

      Row {
        id: rowActions
        anchors.right: parent.right
        anchors.verticalCenter: parent.verticalCenter
        spacing: Style.space(4)
        readonly property double latest: root.latestKnownPoke(friendItem.modelData)
        Text {
          anchors.verticalCenter: parent.verticalCenter
          visible: rowActions.latest > 0
          text: root.relativePokeTime(rowActions.latest, root.labelNow)
          color: root.secondaryForeground
          font.family: root.fontFamily
          font.pixelSize: Style.font.caption
          Accessible.name: root.pokeTimeTooltip(rowActions.latest)
          MouseArea { id: timeMouse; anchors.fill: parent; hoverEnabled: true }
          PanelToolTip {
            visible: timeMouse.containsMouse
            text: root.pokeTimeTooltip(rowActions.latest)
            fontFamily: root.fontFamily
          }
        }
        PokeButton {
          size: Style.space(36)
          fontFamily: root.fontFamily
          foreground: friendItem.hasIncoming ? Color.accent : root.foreground
          sending: friendItem.sendingPoke
          incoming: friendItem.hasIncoming
          waiting: friendItem.waitingForReply
          busy: root.actionRunning
          contactHandle: friendItem.friendHandle
          focusFallback: friendAvatar
          onActiveFocusChanged: if (activeFocus)
            root.ensureListItemVisible(friendItem)
          onClicked: root.runAction("Sending poke…", ["poke", "@" + friendItem.friendHandle])
        }
      }
    }
  }
  Component {
    id: requestRow
    Item {
      required property var modelData
      width: content.width
      height: Math.max(Style.space(38), name.implicitHeight + Style.space(10))
      Text {
        id: name
        anchors.left: parent.left
        anchors.verticalCenter: parent.verticalCenter
        width: Math.max(Style.space(120), parent.width - requestActions.implicitWidth - Style.space(8))
        text: root.displayName(modelData)
        textFormat: Text.PlainText
        elide: Text.ElideRight
        color: root.foreground
        font.family: root.fontFamily
        font.pixelSize: Style.font.body
      }
      Row {
        id: requestActions
        anchors.right: parent.right
        anchors.verticalCenter: parent.verticalCenter
        spacing: Style.space(4)
        Button {
          visible: modelData.outgoing !== 1
          text: "Accept"
          focusable: true
          onClicked: root.runAction("Accepting request…", ["friends", "accept", "@" + root.handle(modelData)])
        }
        Button {
          text: modelData.outgoing === 1 ? "Cancel" : "Decline"
          focusable: true
          onClicked: root.runAction(modelData.outgoing === 1 ? "Canceling request…" : "Declining request…", ["friends", "remove", "@" + root.handle(modelData)])
        }
      }
    }
  }
  Component {
    id: blockedRow
    Item {
      required property var modelData
      width: content.width
      height: Math.max(Style.space(38), name.implicitHeight + Style.space(10))
      Text {
        id: name
        anchors.left: parent.left
        anchors.verticalCenter: parent.verticalCenter
        width: Math.max(Style.space(120), parent.width - action.implicitWidth - Style.space(8))
        text: root.displayName(modelData)
        textFormat: Text.PlainText
        elide: Text.ElideRight
        color: root.foreground
        font.family: root.fontFamily
        font.pixelSize: Style.font.body
      }
      Button {
        id: action
        anchors.right: parent.right
        anchors.verticalCenter: parent.verticalCenter
        text: "Unblock"
        focusable: true
        onClicked: root.runAction("Unblocking…", ["unblock", "@" + root.handle(modelData)])
      }
    }
  }
}
