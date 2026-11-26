import QtQuick
import Quickshell
import Quickshell.Io
import qs.Commons as Commons
import qs.Ui as Ui
import "." as Panora
import "Model.js" as Model
import "PackagePaths.js" as PackagePaths

Ui.BarWidget {
    id: root
    moduleName: "io.github.vadim82.panora"
    property string surface: "preview"
    property bool surfaceOpen: false
    readonly property bool opened: surfaceOpen && surface === "preview"
    readonly property bool menuOpened: surfaceOpen && surface === "menu"
    property bool anchorHadFocus: false
    property bool hideFailed: false
    readonly property string cliExecutable: Model.executablePath(setting("executable", PackagePaths.cli))
    readonly property string uiExecutable: Model.executablePath(setting("uiExecutable", PackagePaths.ui))
    readonly property string serviceSocket: String(setting("socket", ""))
    visible: transport.snapshot.connected
    onVisibleChanged: if (!visible) close()
    implicitWidth: visible ? button.implicitWidth : 0
    implicitHeight: button.implicitHeight

    function setSurface(next) {
        if (!visible || (surfaceOpen && surface === next)) return;
        if (!surfaceOpen) anchorHadFocus = button.activeFocus;
        surface = next;
        surfaceOpen = true;
    }
    function open() { setSurface("preview"); }
    function close() {
        if (!surfaceOpen) return;
        surfaceOpen = false;
        if (visible && anchorHadFocus) button.forceActiveFocus();
    }
    function toggle() { if (opened) close(); else open(); }
    function toggleMenu() {
        if (menuOpened) { close(); return; }
        hideFailed = false;
        setSurface("menu");
    }
    function launchWindow(about) {
        if (!uiExecutable) return;
        var args = [uiExecutable];
        if (about) args.push("--about");
        if (serviceSocket) args.push("--socket", serviceSocket);
        Quickshell.execDetached(args);
        close();
    }
    function hideFromBar() {
        if (disableWidget.running) return;
        hideFailed = false;
        disableWidget.running = true;
        disableDeadline.start();
    }
    Process {
        id: disableWidget
        command: ["omarchy", "plugin", "disable", root.moduleName]
        onExited: function(code, status) { disableDeadline.stop(); root.hideFailed = code !== 0 || status !== 0; }
    }
    Timer {
        id: disableDeadline
        interval: 7500
        onTriggered: { root.hideFailed = true; disableWidget.signal(9); }
    }
    Commons.Translations { id: strings; directory: Qt.resolvedUrl("i18n") }
    Commons.ShellIpc {
        target: root.moduleName
        function widgetSource(): string { return Qt.resolvedUrl("BarWidget.qml").toString(); }
    }
    Panora.Controller {
        id: transport
        executable: root.cliExecutable
        uiExecutable: root.uiExecutable
        socket: root.serviceSocket
        previewWanted: root.opened
    }
    Ui.BarIconButton {
        id: button
        activeFocusOnTab: true
        Keys.onReturnPressed: root.toggle()
        Keys.onEnterPressed: root.toggle()
        Keys.onSpacePressed: root.toggle()
        Keys.onPressed: function(event) {
            if (event.key === Qt.Key_Menu || (event.key === Qt.Key_F10 && event.modifiers === Qt.ShiftModifier)) {
                root.toggleMenu();
                event.accepted = true;
            }
        }
        anchors.fill: parent
        bar: root.bar
        text: "󰸉"
        active: root.surfaceOpen
        tooltipText: strings.t(root.opened ? "close" : "showPopup")
        Accessible.role: Accessible.Button
        Accessible.name: tooltipText
        Accessible.onPressAction: root.toggle()
        onPressed: function(mouseButton) {
            if (mouseButton === Qt.LeftButton) root.toggle();
            if (mouseButton === Qt.RightButton) root.toggleMenu();
        }
    }
    Ui.KeyboardPanel {
        id: panel
        anchorItem: button
        owner: root
        bar: root.bar
        open: root.surfaceOpen
        contentWidth: panel.fittedContentWidth(Commons.Style.space(root.surface === "menu" ? 200 : 360))
        contentHeight: panel.fittedContentHeight(panel.loadedContentHeight)
        focusTarget: panel.loadedContent ? panel.loadedContent.item : null
        content: Item {
            property alias item: surfaceLoader.item
            implicitHeight: item ? item.implicitHeight : 0
            Loader {
                id: surfaceLoader
                anchors.fill: parent
                sourceComponent: root.surface === "menu" ? menu : preview
                onLoaded: if (root.surfaceOpen) Qt.callLater(function() {
                    if (item && root.surfaceOpen) item.forceActiveFocus();
                })
            }
        }
    }
    Component {
        id: preview
        Panora.PopupContent {
            controller: transport
            displaying: root.opened
            cliConfigured: root.cliExecutable !== ""
            canOpenWindow: root.uiExecutable !== ""
            onOpenRequested: root.launchWindow(false)
            onCloseRequested: root.close()
            onMenuRequested: root.toggleMenu()
        }
    }
    Component {
        id: menu
        Panora.ContextMenuContent {
            displaying: root.menuOpened
            canOpenWindow: root.uiExecutable !== ""
            canHide: !disableWidget.running
            canQuit: Model.allowed(transport.snapshot, "stop") && !transport.actionInFlight
            errorKey: root.hideFailed ? "hideFailed" : transport.actionError
            onCloseRequested: root.close()
            onOpenRequested: root.launchWindow(false)
            onAboutRequested: root.launchWindow(true)
            onHideRequested: root.hideFromBar()
            onQuitRequested: transport.act("stop")
        }
    }
}
