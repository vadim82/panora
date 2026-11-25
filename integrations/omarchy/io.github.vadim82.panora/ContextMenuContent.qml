import QtQuick
import qs.Commons as Commons
import qs.Ui
import "." as Panora

FocusScope {
    id: root
    property bool displaying: true
    property bool canOpenWindow: true
    property bool canHide: true
    property bool canQuit: true
    property string errorKey: ""
    signal closeRequested()
    signal openRequested()
    signal aboutRequested()
    signal hideRequested()
    signal quitRequested()
    implicitHeight: column.implicitHeight
    Accessible.role: Accessible.PopupMenu
    Accessible.name: strings.t("menu")

    function focusNext(step) {
        var count = actions.count;
        var current = step > 0 ? -1 : 0;
        for (var i = 0; i < count; i++) {
            if (actions.itemAt(i).activeFocus) current = i;
        }
        for (var offset = 1; offset <= count; offset++) {
            var item = actions.itemAt((current + step * offset + count) % count);
            if (item.enabled) { item.forceActiveFocus(); return; }
        }
    }
    function invoke(action) {
        switch (action) {
        case "open": openRequested(); break;
        case "about": aboutRequested(); break;
        case "hide": hideRequested(); break;
        case "quit": quitRequested(); break;
        }
    }
    onDisplayingChanged: if (displaying) focusNext(1)
    Component.onCompleted: if (displaying) focusNext(1)
    Keys.onEscapePressed: function(event) { closeRequested(); event.accepted = true; }
    Keys.onPressed: function(event) {
        if (event.key === Qt.Key_Menu || (event.key === Qt.Key_F10 && event.modifiers === Qt.ShiftModifier)) closeRequested();
        else if (event.key === Qt.Key_Down || event.key === Qt.Key_Tab) focusNext(1);
        else if (event.key === Qt.Key_Up || event.key === Qt.Key_Backtab) focusNext(-1);
        else return;
        event.accepted = true;
    }

    Commons.Translations { id: strings; directory: Qt.resolvedUrl("i18n") }
    Column {
        id: column
        width: parent.width
        spacing: Commons.Style.space(3)
        Repeater {
            id: actions
            model: ["open", "about", "hide", "quit"]
            Button {
                required property string modelData
                width: column.width
                text: strings.t(modelData)
                foreground: Commons.Color.popups.text
                leftAlign: true
                focusable: true
                enabled: modelData === "quit" ? root.canQuit : modelData === "hide" ? root.canHide : root.canOpenWindow
                opacity: enabled ? 1 : .45
                Accessible.role: Accessible.MenuItem
                Accessible.name: text
                Accessible.onPressAction: if (enabled) root.invoke(modelData)
                onClicked: root.invoke(modelData)
            }
        }
        Panora.PlainText {
            width: parent.width
            visible: root.errorKey !== ""
            text: visible ? strings.t(root.errorKey) : ""
            font.pixelSize: Commons.Style.space(11)
        }
    }
}
