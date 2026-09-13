import QtQuick
import qs.Ui

// KeyboardPanel currently keeps its card radius private. Adapt only that
// surface, leaving native positioning, focus and popout coordination intact.
KeyboardPanel {
  Component.onCompleted: {
    var child = contentItem.length ? contentItem[0].parent : null
    while (child) {
      if (child instanceof BorderSurface && "contentTopInset" in child) {
        child.radius = 0
        return
      }
      child = child.parent
    }
    console.warn("Pokachy: native panel card unavailable; square corners could not be applied")
  }
}
