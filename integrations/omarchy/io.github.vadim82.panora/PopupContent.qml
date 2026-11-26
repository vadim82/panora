import QtQuick
import qs.Commons as Commons
import qs.Ui
import "Model.js" as Model
import "." as Panora

FocusScope {
    id: root
    required property var controller
    property bool displaying: true
    property bool cliConfigured: true
    property bool canOpenWindow: false
    readonly property var status: controller.snapshot
    readonly property var image: controller.displayedImage
    readonly property bool canPrevious: Model.allowed(status, "previous") && !controller.actionInFlight
    readonly property bool canNext: Model.allowed(status, "next") && !controller.actionInFlight
    readonly property string modeKey: cliConfigured ? Model.stateKey(status) : "executableRequired"
    readonly property bool canRefresh: cliConfigured && !controller.actionInFlight &&
        (!!controller.actionError || !status.connected || status.warning)
    readonly property string noticeKey: controller.actionError || (!cliConfigured ? "executableNotice"
        : !status.connected ? "disconnectedNotice"
        : status.uncertain ? "uncertainNotice"
        : status.warning ? "warning" : "")
    readonly property string modeText: {
        if (controller.actionInFlight) return strings.t("busy");
        if (modeKey === "paused") return "";
        if (modeKey !== "running") return strings.t(modeKey);
        if (!status.next_at) return "";
        var value = new Date(status.next_at);
        return isNaN(value.getTime()) ? ""
            : strings.t("nextAt").arg(value.toLocaleTimeString(Qt.locale(Commons.I18n.formatLocale), "HH:mm"));
    }
    signal closeRequested()
    signal menuRequested()
    signal openRequested()
    implicitHeight: column.implicitHeight
    Accessible.role: Accessible.Grouping
    Accessible.name: "Panora"
    Keys.priority: Keys.AfterItem
    Keys.onEscapePressed: function(event) { root.closeRequested(); event.accepted = true; }
    Keys.onPressed: function(event) {
        if (event.key === Qt.Key_Menu || (event.key === Qt.Key_F10 && event.modifiers === Qt.ShiftModifier)) {
            root.menuRequested();
            event.accepted = true;
        } else if (root.displaying && event.modifiers === Qt.NoModifier &&
                   (event.key === Qt.Key_Left || event.key === Qt.Key_Right)) {
            var previous = event.key === Qt.Key_Left;
            if (!event.isAutoRepeat && (previous ? root.canPrevious : root.canNext))
                root.controller.act(previous ? "previous" : "next");
            event.accepted = true;
        }
    }
    function focusFirst() {
        if (root.canNext) flow.focusCard("next");
        else if (refreshButton.enabled) refreshButton.forceActiveFocus();
        else closeButton.forceActiveFocus();
    }
    // Prime the child once per opening. The host focuses this scope when
    // the layer maps, and Qt retains the user's selected child on focus return.
    onDisplayingChanged: if (displaying) focusFirst()
    Component.onCompleted: if (displaying) focusFirst()

    Commons.Translations { id: strings; directory: Qt.resolvedUrl("i18n") }
    Column {
        id: column
        width: parent.width
        spacing: Commons.Style.space(12)
        Row {
            width: parent.width
            spacing: Commons.Style.space(4)
            Panora.PlainText {
                width: parent.width - closeButton.width - parent.spacing
                text: "Panora"
                font.pixelSize: Commons.Style.font.heading
                font.weight: Font.DemiBold
                maximumLineCount: 1
                elide: Text.ElideRight
                anchors.verticalCenter: parent.verticalCenter
            }
            Button {
                id: closeButton
                text: "×"
                foreground: Commons.Color.popups.text
                anchors.verticalCenter: parent.verticalCenter
                focusable: true
                tooltipText: strings.t("close")
                Accessible.role: Accessible.Button
                Accessible.name: strings.t("close")
                Accessible.onPressAction: root.closeRequested()
                onClicked: root.closeRequested()
            }
        }
        Panora.PlainText {
            width: parent.width
            visible: !root.status.connected && !!root.image
            text: strings.t("lastKnown")
            horizontalAlignment: Text.AlignHCenter
            font.pixelSize: Commons.Style.font.caption
            opacity: .8
        }
        Panora.CoverFlow {
            id: flow
            width: parent.width
            height: Math.min(Commons.Style.space(156), width * 156 / 360)
            current: root.displaying ? root.image : null
            instance: root.displaying ? root.status.instance_id || "" : ""
            previous: root.displaying ? root.controller.previousPreview : null
            next: root.displaying ? root.controller.nextPreview : null
            showPrevious: root.status.connected && root.status.can_previous && !root.status.setup_required
            showNext: root.status.connected && !root.status.setup_required
            showNextPlaceholder: showNext && !!root.image && root.displaying
            previousEnabled: root.canPrevious
            nextEnabled: root.canNext
            previousLabel: strings.t("previousWallpaper")
            nextLabel: strings.t("next")
            currentLabel: strings.t("previewDescription")
            fallback: root.image ? strings.t("previewUnavailable") : strings.t("noImage")
            onNavigate: function(action) { root.controller.act(action); }
        }
        Column {
            width: parent.width
            spacing: Commons.Style.space(4)
            Panora.PlainText {
                width: parent.width
                visible: !!root.image
                text: root.image ? root.image.title : ""
                horizontalAlignment: Text.AlignHCenter
                font.pixelSize: Commons.Style.font.heading
                font.weight: Font.DemiBold
                maximumLineCount: 2
                elide: Text.ElideRight
            }
            Panora.PlainText {
                width: parent.width
                visible: text !== ""
                text: {
                    if (!root.image) return "";
                    var ratio = Model.aspectRatio(root.image.width, root.image.height);
                    if (!ratio) return "";
                    var dimensions = strings.t("dimensions").arg(Commons.I18n.formatNumber(root.image.width)).arg(Commons.I18n.formatNumber(root.image.height));
                    return dimensions + " · " + ratio;
                }
                horizontalAlignment: Text.AlignHCenter
                font.pixelSize: Commons.Style.font.bodySmall
                opacity: .8
                maximumLineCount: 2
                elide: Text.ElideRight
            }
            Panora.PlainText {
                width: parent.width
                visible: text !== ""
                text: root.image && root.image.origin ? (root.image.source_name
                    ? root.image.origin + " · " + root.image.source_name
                    : root.image.origin) : ""
                horizontalAlignment: Text.AlignHCenter
                font.pixelSize: Commons.Style.font.bodySmall
                opacity: .8
                maximumLineCount: 2
                elide: Text.ElideRight
            }
        }
        Button {
            id: rotationButton
            anchors.horizontalCenter: parent.horizontalCenter
            width: Math.min(parent.width, implicitWidth)
            iconText: root.status.paused ? "▶" : "❚❚"
            text: tooltipText
            foreground: Commons.Color.popups.text
            bordered: true
            focusable: true
            readonly property string action: root.status.paused ? "resume" : "pause"
            enabled: Model.allowed(root.status, action) && !root.controller.actionInFlight
            opacity: enabled ? 1 : .45
            tooltipText: strings.t(action)
            Accessible.role: Accessible.Button
            Accessible.name: tooltipText
            Accessible.onPressAction: root.controller.act(action)
            onClicked: root.controller.act(action)
        }
        Panora.PlainText {
            width: parent.width
            visible: text !== ""
            horizontalAlignment: Text.AlignHCenter
            font.pixelSize: Commons.Style.font.bodySmall
            text: root.modeText
            opacity: .85
        }
        Button {
            anchors.horizontalCenter: parent.horizontalCenter
            width: Math.min(parent.width, implicitWidth)
            visible: root.canOpenWindow
            text: strings.t("open")
            foreground: Commons.Color.popups.text
            fontSize: Commons.Style.font.bodySmall
            focusable: true
            Accessible.role: Accessible.Button
            Accessible.name: text
            Accessible.onPressAction: root.openRequested()
            onClicked: root.openRequested()
        }
        Rectangle {
            width: parent.width
            height: notice.implicitHeight + Commons.Style.space(20)
            visible: root.noticeKey !== ""
            color: Qt.rgba(Commons.Color.urgent.r, Commons.Color.urgent.g, Commons.Color.urgent.b, .08)
            border.color: Commons.Color.urgent
            border.width: 1
            radius: Commons.Style.cornerRadius
            Panora.PlainText {
                id: notice
                anchors.left: parent.left
                anchors.right: parent.right
                anchors.top: parent.top
                anchors.margins: Commons.Style.space(10)
                text: root.noticeKey ? strings.t(root.noticeKey) : ""
                font.pixelSize: Commons.Style.font.body
                Accessible.role: Accessible.StaticText
                Accessible.name: text
            }
        }
        Button {
            id: refreshButton
            text: strings.t("refresh")
            foreground: Commons.Color.popups.text
            focusable: true
            visible: root.canRefresh
            enabled: visible
            tooltipText: strings.t("refresh")
            Accessible.role: Accessible.Button
            Accessible.name: text
            Accessible.onPressAction: root.controller.retryPreview()
            onClicked: root.controller.retryPreview()
        }
    }
}
