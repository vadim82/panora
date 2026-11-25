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
    readonly property var status: controller.snapshot
    readonly property var image: controller.displayedImage
    readonly property string modeKey: cliConfigured ? Model.stateKey(status) : "executableRequired"
    readonly property bool canRefresh: cliConfigured && !controller.actionInFlight &&
        (!!controller.actionError || !status.connected || status.warning)
    readonly property string noticeKey: controller.actionError || (!cliConfigured ? "executableNotice"
        : !status.connected ? "disconnectedNotice"
        : status.uncertain ? "uncertainNotice" : status.busy || controller.actionInFlight ? "busyNotice"
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
    implicitHeight: column.implicitHeight
    Accessible.role: Accessible.Grouping
    Accessible.name: "Panora"
    Keys.onEscapePressed: function(event) { root.closeRequested(); event.accepted = true; }
    Keys.onPressed: function(event) {
        if (event.key === Qt.Key_Menu || (event.key === Qt.Key_F10 && event.modifiers === Qt.ShiftModifier)) {
            root.menuRequested();
            event.accepted = true;
        }
    }
    function accentLabelColor(accent) {
        function linear(channel) {
            return channel <= 0.04045 ? channel / 12.92 : Math.pow((channel + 0.055) / 1.055, 2.4);
        }
        var luminance = 0.2126 * linear(accent.r) + 0.7152 * linear(accent.g) + 0.0722 * linear(accent.b);
        // Black and white have equal contrast at this luminance.
        return luminance > 0.179 ? "black" : "white";
    }
    function focusFirst() {
        if (nextButton.enabled) nextButton.forceActiveFocus();
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
                font.family: "serif"
                font.pixelSize: Commons.Style.space(26)
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
            text: root.status.connected ? strings.t("lastAccepted") : strings.t("lastKnown")
            font.pixelSize: Commons.Style.space(10)
            font.letterSpacing: 1
            opacity: .8
        }
        Rectangle {
            width: parent.width
            height: Commons.Style.space(168)
            color: Commons.Color.popups.background
            radius: Commons.Style.cornerRadius
            Image {
                id: picture
                anchors.fill: parent
                anchors.margins: Commons.Style.space(2)
                source: root.displaying && root.image && root.image.data_uri ? root.image.data_uri : ""
                sourceSize.width: 512
                sourceSize.height: 512
                fillMode: Image.PreserveAspectFit
                asynchronous: true
                cache: false
                Accessible.role: Accessible.Graphic
                Accessible.name: strings.t("previewDescription")
            }
            Panora.PlainText {
                anchors.fill: parent
                anchors.margins: Commons.Style.space(14)
                horizontalAlignment: Text.AlignHCenter
                verticalAlignment: Text.AlignVCenter
                visible: picture.status !== Image.Ready
                text: root.image ? strings.t("previewUnavailable") : strings.t("noImage")
            }
        }
        Column {
            width: parent.width
            spacing: Commons.Style.space(4)
            Panora.PlainText {
                width: parent.width
                text: root.image ? root.image.title : strings.t("noImage")
                font.family: "serif"
                font.pixelSize: Commons.Style.space(23)
                maximumLineCount: 2
                elide: Text.ElideRight
            }
            Panora.PlainText {
                width: parent.width
                visible: !!root.image
                text: root.image ? root.image.origin : ""
                font.pixelSize: Commons.Style.space(12)
                opacity: .8
                maximumLineCount: 2
                elide: Text.ElideRight
            }
            Panora.PlainText {
                width: parent.width
                visible: !!root.image && root.image.width > 0 && root.image.height > 0
                text: root.image ? strings.t("dimensions").arg(Commons.I18n.formatNumber(root.image.width)).arg(Commons.I18n.formatNumber(root.image.height)) : ""
                font.pixelSize: Commons.Style.space(11)
                opacity: .8
            }
        }
        Row {
            id: controls
            width: parent.width
            height: Math.max(previousButton.implicitHeight, rotationButton.implicitHeight, nextButton.implicitHeight)
            spacing: Commons.Style.space(7)
            readonly property real navigationWidth: (width - rotationButton.width - spacing * 2) / 2
            Button {
                id: previousButton
                width: controls.navigationWidth
                height: controls.height
                verticalPadding: Commons.Style.space(10)
                text: "‹ " + strings.t("previous")
                foreground: Commons.Color.popups.text
                bordered: true
                focusable: true
                enabled: Model.allowed(root.status, "previous") && !root.controller.actionInFlight
                opacity: enabled ? 1 : .45
                tooltipText: strings.t("previousWallpaper")
                Accessible.role: Accessible.Button
                Accessible.name: tooltipText
                Accessible.onPressAction: root.controller.act("previous")
                onClicked: root.controller.act("previous")
            }
            Button {
                id: rotationButton
                width: Math.min(Commons.Style.space(51), controls.width / 3)
                height: controls.height
                verticalPadding: Commons.Style.space(10)
                iconText: root.status.paused ? "▶" : "❚❚"
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
            Rectangle {
                width: controls.navigationWidth
                height: controls.height
                radius: Commons.Style.cornerRadius
                color: Commons.Color.accent
                opacity: nextButton.enabled ? 1 : .45
                Button {
                    id: nextButton
                    anchors.fill: parent
                    verticalPadding: Commons.Style.space(10)
                    text: strings.t("nextShort") + " ›"
                    foreground: root.accentLabelColor(Commons.Color.accent)
                    bordered: true
                    focusable: true
                    enabled: Model.allowed(root.status, "next") && !root.controller.actionInFlight
                    tooltipText: strings.t("next")
                    Accessible.role: Accessible.Button
                    Accessible.name: tooltipText
                    Accessible.onPressAction: root.controller.act("next")
                    onClicked: root.controller.act("next")
                }
            }
        }
        Panora.PlainText {
            width: parent.width
            visible: text !== ""
            horizontalAlignment: Text.AlignHCenter
            font.pixelSize: Commons.Style.space(11)
            text: root.modeText
            opacity: .85
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
                font.pixelSize: Commons.Style.space(12)
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
