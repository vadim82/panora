import QtQuick
import QtQuick.Window
import QtTest
import Quickshell
import "Panora" as Panora

ShellRoot {
    Timer {
        interval: 100
        running: true
        onTriggered: {
            try {
                keyboard.checkControls();
            } catch (error) {
                console.error(error.stack);
                Qt.exit(1);
            }
        }
    }
    Window {
        id: window
        visible: true
        width: 360
        height: 800
        Item {
            id: transport
            property var snapshot: ({connected:true,instance_id:"one",paused:true,busy:false,uncertain:false,setup_required:false,can_previous:true,warning:false})
            property var displayedImage: null
            property var previousPreview: null
            property var nextPreview: null
            property bool actionInFlight: false
            property string actionError: ""
            property string lastAction: ""
            property int actions: 0
            property int refreshes: 0
            function act(name) { lastAction = name; actions++; }
            function retryPreview() { refreshes++; }
        }
        Item { id: outside }
        Panora.PopupContent {
            id: popup
            width: parent.width
            controller: transport
            property bool closed: false
            property int menuRequests: 0
            property int openRequests: 0
            onCloseRequested: closed = true
            onOpenRequested: openRequests++
            onMenuRequested: menuRequests++
        }
        Panora.ContextMenuContent {
            id: menu
            width: 200
            visible: false
            displaying: visible
            property string invoked: ""
            onCloseRequested: invoked = "close"
            onOpenRequested: invoked = "open"
            onAboutRequested: invoked = "about"
            onHideRequested: invoked = "hide"
            onQuitRequested: invoked = "quit"
        }
        TestCase {
            id: keyboard
            name: "PopupKeyboard"
            when: false
            function checkControls() {
                popup.forceActiveFocus();
                tryCompare(window.activeFocusItem, "label", "Next wallpaper");
                keyClick(Qt.Key_Return);
                compare(transport.lastAction, "next");
                keyClick(Qt.Key_Menu);
                keyClick(Qt.Key_F10, Qt.ShiftModifier);
                compare(popup.menuRequests, 2, "The focused wallpaper popup can open its context menu");
                var actions = transport.actions;
                keyClick(Qt.Key_Left);
                compare(transport.lastAction, "previous");
                keyClick(Qt.Key_Right);
                compare(transport.lastAction, "next");
                compare(transport.actions, actions + 2, "Arrow navigation sends one action each");
                transport.snapshot = Object.assign({}, transport.snapshot, {can_previous:false});
                keyClick(Qt.Key_Left);
                compare(transport.actions, actions + 2, "Left is unavailable at the beginning of History");
                transport.snapshot = Object.assign({}, transport.snapshot, {can_previous:true});
                compare(window.activeFocusItem.label, "Next wallpaper", "Arrow navigation preserves focus");
                compare(popup.modeText, "", "The play icon communicates ordinary pause");
                keyClick(Qt.Key_Tab);
                compare(window.activeFocusItem.tooltipText, "Resume rotation");
                keyClick(Qt.Key_Space);
                compare(transport.lastAction, "resume");
                transport.snapshot = Object.assign({}, transport.snapshot, {paused:false, next_at:"2026-11-23T12:34:00Z"});
                verify(popup.modeText.startsWith("Next change: "));
                compare(window.activeFocusItem.tooltipText, "Pause rotation");
                keyClick(Qt.Key_Return);
                compare(transport.lastAction, "pause");
                keyClick(Qt.Key_Backtab, Qt.ShiftModifier);
                keyClick(Qt.Key_Backtab, Qt.ShiftModifier);
                keyClick(Qt.Key_Return);
                compare(transport.lastAction, "previous");
                keyClick(Qt.Key_Tab);
                keyClick(Qt.Key_Backtab, Qt.ShiftModifier);
                outside.forceActiveFocus();
                popup.forceActiveFocus();
                wait(25);
                transport.lastAction = "";
                keyClick(Qt.Key_Return);
                compare(transport.lastAction, "previous", "Focus restoration keeps the selected control");
                transport.snapshot = Object.assign({}, transport.snapshot, {can_previous:false});
                tryCompare(window.activeFocusItem, "label", "Next wallpaper");
                transport.snapshot = Object.assign({}, transport.snapshot, {can_previous:true});
                keyClick(Qt.Key_Escape);
                compare(popup.closed, true);
                popup.closed = false;
                popup.focusFirst();
                keyClick(Qt.Key_Tab);
                compare(window.activeFocusItem.tooltipText, "Pause rotation");
                keyClick(Qt.Key_Tab);
                compare(window.activeFocusItem.text, "×", "Healthy preview offers only playback and Close");
                popup.focusFirst();
                transport.actionInFlight = true;
                compare(popup.modeText, "Working…");
                compare(popup.noticeKey, "", "An in-flight action is ordinary progress");
                wait(0);
                keyClick(Qt.Key_Escape);
                compare(popup.closed, true, "Escape works while the focused action is in flight");
                popup.closed = false;
                transport.actionInFlight = false;
                transport.snapshot = Object.assign({}, transport.snapshot, {busy:true});
                compare(popup.modeText, "Working…");
                compare(popup.noticeKey, "", "A busy service is ordinary progress");
                transport.snapshot = Object.assign({}, transport.snapshot, {busy:false});
                transport.actionError = "actionUnconfirmed";
                compare(popup.noticeKey, "actionUnconfirmed");
                popup.displaying = false;
                popup.displaying = true;
                keyClick(Qt.Key_Tab);
                keyClick(Qt.Key_Tab);
                compare(window.activeFocusItem.text, "Check status");
                keyClick(Qt.Key_Return);
                compare(transport.refreshes, 1);
                transport.actionError = "";
                popup.cliConfigured = false;
                transport.snapshot = {connected:false};
                compare(popup.modeKey, "executableRequired");
                compare(popup.noticeKey, "executableNotice");
                popup.displaying = false;
                popup.displaying = true;
                keyClick(Qt.Key_Return);
                compare(popup.closed, true);
                popup.cliConfigured = true;
                transport.snapshot = {connected:true,instance_id:"one",paused:true,busy:false,uncertain:false,
                    setup_required:false,can_previous:false,warning:false};
                popup.width = 224;
                popup.canOpenWindow = true;
                popup.focusFirst();
                keyClick(Qt.Key_Tab);
                compare(window.activeFocusItem.text, "Resume rotation");
                keyClick(Qt.Key_Tab);
                compare(window.activeFocusItem.text, "Open Panora");
                keyClick(Qt.Key_Return);
                keyClick(Qt.Key_Space);
                var openButton = window.activeFocusItem;
                tryVerify(function() { return openButton.y > 0; });
                mouseClick(openButton, openButton.width / 2, openButton.height / 2);
                compare(popup.openRequests, 3, "The narrow popup offers the same launcher by pointer and keyboard");
                popup.visible = false;
                menu.visible = true;
                menu.forceActiveFocus();
                compare(window.activeFocusItem.text, "Open Panora");
                keyClick(Qt.Key_Return);
                compare(menu.invoked, "open");
                keyClick(Qt.Key_Down);
                compare(window.activeFocusItem.text, "About Panora");
                keyClick(Qt.Key_Space);
                compare(menu.invoked, "about");
                keyClick(Qt.Key_Tab);
                compare(window.activeFocusItem.text, "Hide from bar");
                keyClick(Qt.Key_Return);
                compare(menu.invoked, "hide");
                keyClick(Qt.Key_Down);
                compare(window.activeFocusItem.text, "Quit Panora");
                keyClick(Qt.Key_Return);
                compare(menu.invoked, "quit");
                keyClick(Qt.Key_Down);
                compare(window.activeFocusItem.text, "Open Panora");
                menu.canQuit = false;
                keyClick(Qt.Key_Up);
                compare(window.activeFocusItem.text, "Hide from bar");
                keyClick(Qt.Key_Escape);
                compare(menu.invoked, "close");
                console.log("PANORA_TEST " + JSON.stringify({pass:true,keyboard:"Return Space Tab Shift-Tab Escape"}));
                Qt.quit();
            }
        }
    }
}
