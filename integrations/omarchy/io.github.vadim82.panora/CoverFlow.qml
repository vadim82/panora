import QtQuick
import QtQuick.Controls as Controls
import qs.Commons as Commons
import "." as Panora

Item {
    id: root
    property var current: null
    property var previous: null
    property var next: null
    property string instance: ""
    property bool showPrevious: false
    property bool showNext: false
    property bool showNextPlaceholder: false
    property bool previousEnabled: false
    property bool nextEnabled: false
    property string previousLabel: ""
    property string nextLabel: ""
    property string currentLabel: ""
    property string fallback: ""
    signal navigate(string action)

    readonly property real peek: width * .175
    readonly property color shadow: Qt.darker(Commons.Color.popups.background, 2)
    readonly property real centerWidth: left.ready || right.ready || showNextPlaceholder ? width - 2 * peek : width - Commons.Style.space(12)
    function focusCard(action) {
        (action === "previous" ? previousTarget : nextTarget).forceActiveFocus();
    }
    onShowPreviousChanged: if (!showPrevious && showNext && previousTarget.activeFocus) focusCard("next")
    property var accepted: null
    property string acceptedInstance: ""
    property real progress: 1
    property int direction: 0
    clip: true

    NumberAnimation on progress {
        id: slide
        running: false
        from: 0
        to: 1
        duration: Commons.Style.duration(280)
        easing.type: Easing.OutCubic
    }

    function reveal() {
        if (picture.status !== Image.Ready || !current || !instance) return;
        var old = accepted;
        var sameInstance = acceptedInstance === instance;
        accepted = current;
        acceptedInstance = instance;
        if (!sameInstance || !old || old.history_id === current.history_id || Commons.Style.reduceMotion) return;
        direction = next && (next.history_id ? next.history_id === old.history_id : next.id === old.id) ? -1 : 1;
        slide.restart();
    }
    onCurrentChanged: Qt.callLater(reveal)
    onInstanceChanged: {
        slide.stop();
        progress = 1;
        accepted = null;
        acceptedInstance = "";
        Qt.callLater(reveal);
    }

    component SideCard: Item {
        id: side
        required property var preview
        required property int direction
        property bool highlighted: false
        property bool placeholder: false
        readonly property bool ready: wallpaper.status === Image.Ready && !!preview

        width: root.width * .4
        height: root.height * .64
        anchors.verticalCenter: parent.verticalCenter
        x: direction < 0 ? Commons.Style.space(11) : parent.width - width - Commons.Style.space(11)
        visible: ready || placeholder
        // Project the Y-axis turn around the center with a short viewing
        // distance so the perspective is visible at popup scale.
        transform: Matrix4x4 {
            readonly property real cx: side.width / 2
            readonly property real cy: side.height / 2
            readonly property real depth: -side.direction * .52 / Math.max(1, side.width)
            readonly property real flatten: .8
            matrix: Qt.matrix4x4(
                flatten + cx * depth, 0, 0, cx * (1 - flatten - cx * depth),
                cy * depth, 1, 0, -cy * cx * depth,
                0, 0, 1, 0,
                depth, 0, 0, 1 - cx * depth)
        }

        Rectangle {
            anchors.fill: wallpaper
            anchors.topMargin: Commons.Style.space(5)
            anchors.bottomMargin: -Commons.Style.space(5)
            color: Commons.Util.alpha(root.shadow, .19)
            radius: Commons.Style.cornerRadius
        }

        Rectangle {
            anchors.fill: parent
            visible: side.placeholder && !side.ready
            color: Commons.Style.hoverFillFor(Commons.Color.popups.text, Commons.Color.accent)
            border.width: 1
            border.color: Commons.Color.popups.border
        }
        Image {
            id: wallpaper
            anchors.fill: parent
            source: side.preview ? side.preview.data_uri : ""
            sourceSize.width: 512
            sourceSize.height: 512
            fillMode: Image.PreserveAspectCrop
            asynchronous: true
            cache: false
            visible: side.ready
            clip: true
        }
        Rectangle {
            anchors.fill: parent
            visible: side.ready
            color: Commons.Util.alpha(Commons.Color.popups.background, side.highlighted ? .035 : .21)
            border.width: 1
            border.color: side.highlighted ? Commons.Color.accent : Commons.Util.alpha(Commons.Color.popups.border, .35)
            Behavior on color { ColorAnimation { duration: Commons.Style.duration(140) } }
        }
    }

    SideCard {
        id: left
        direction: -1
        preview: root.previous
        highlighted: previousTarget.highlighted
    }
    SideCard {
        id: right
        direction: 1
        preview: root.next
        placeholder: root.showNextPlaceholder
        highlighted: nextTarget.highlighted
    }
    Item {
        id: center
        x: (root.width - width) / 2 + root.direction * root.peek * (1 - root.progress)
        width: root.centerWidth
        height: root.height
        anchors.verticalCenter: parent.verticalCenter
        scale: .78 + .22 * root.progress
        transform: Rotation {
            origin.x: center.width / 2
            origin.y: center.height / 2
            axis.y: 1
            axis.z: 0
            angle: root.direction * 24 * (1 - root.progress)
        }
        Rectangle {
            width: picture.paintedWidth + Commons.Style.space(6)
            height: picture.paintedHeight + Commons.Style.space(7)
            x: (parent.width - width) / 2
            y: (parent.height - picture.paintedHeight) / 2
            radius: Commons.Style.cornerRadius
            color: Commons.Util.alpha(root.shadow, .11)
            visible: picture.status === Image.Ready
        }
        Rectangle {
            width: picture.paintedWidth + Commons.Style.space(2)
            height: picture.paintedHeight + Commons.Style.space(4)
            x: (parent.width - width) / 2
            y: (parent.height - picture.paintedHeight) / 2
            radius: Commons.Style.cornerRadius
            color: Commons.Util.alpha(root.shadow, .15)
            visible: picture.status === Image.Ready
        }
        Image {
            id: picture
            anchors.fill: parent
            source: root.current && root.current.data_uri ? root.current.data_uri : ""
            sourceSize.width: 512
            sourceSize.height: 512
            fillMode: Image.PreserveAspectFit
            asynchronous: true
            cache: false
            Accessible.role: Accessible.Graphic
            Accessible.name: root.currentLabel
            onStatusChanged: if (status === Image.Ready) Qt.callLater(root.reveal)
        }
        Panora.PlainText {
            anchors.fill: parent
            anchors.margins: Commons.Style.space(14)
            horizontalAlignment: Text.AlignHCenter
            verticalAlignment: Text.AlignVCenter
            visible: picture.status !== Image.Ready
            text: root.fallback
        }
    }

    component SideTarget: Item {
        id: target
        required property string action
        required property string label
        required property int direction
        property bool hasCard: false
        property bool hasPlaceholder: false
        property bool available: false
        readonly property bool highlighted: activeFocus || hover.hovered

        width: Math.max(root.peek, Commons.Style.space(44))
        height: hasCard || hasPlaceholder ? root.height * .87 : Commons.Style.space(44)
        anchors.verticalCenter: parent.verticalCenter
        Accessible.role: Accessible.Button
        Accessible.name: label
        Accessible.ignored: !visible
        Accessible.onPressAction: activate()
        activeFocusOnTab: true
        function activate() {
            if (visible && available) root.navigate(action);
        }
        Keys.onReturnPressed: function(event) { if (!event.isAutoRepeat) activate(); }
        Keys.onEnterPressed: function(event) { if (!event.isAutoRepeat) activate(); }
        Keys.onSpacePressed: function(event) { if (!event.isAutoRepeat) activate(); }
        HoverHandler { id: hover; enabled: target.available; cursorShape: Qt.PointingHandCursor }
        TapHandler { id: tap; enabled: target.available; onTapped: target.activate() }
        Rectangle {
            anchors.centerIn: parent
            width: Commons.Style.space(27)
            height: width
            radius: width / 2
            color: {
                var background = Commons.Color.popups.background;
                var text = Commons.Color.popups.text;
                var accent = Commons.Color.accent;
                if (tap.pressed) return Qt.tint(background, Commons.Style.pressedFillFor(text, accent));
                if (target.activeFocus) return Qt.tint(background, Commons.Style.focusFillFor(text, accent));
                if (hover.hovered) return Qt.tint(background, Commons.Style.hoverFillFor(text, accent));
                return background;
            }
            opacity: target.available ? 1 : .65
            scale: tap.pressed ? .9 : hover.hovered ? 1.08 : 1
            Behavior on scale {
                NumberAnimation { duration: Commons.Style.duration(120); easing.type: Easing.OutCubic }
            }
            border.width: 1
            border.color: target.highlighted ? Commons.Color.accent
                : target.hasCard ? "transparent" : Commons.Color.popups.border
            Canvas {
                anchors.centerIn: parent
                width: Commons.Style.space(12)
                height: width
                readonly property color strokeColor: Commons.Color.popups.text
                onStrokeColorChanged: requestPaint()
                onPaint: {
                    var context = getContext("2d");
                    context.clearRect(0, 0, width, height);
                    var center = width / 2;
                    var offset = target.direction * width / 6;
                    context.beginPath();
                    context.moveTo(center - offset, height * .22);
                    context.lineTo(center + offset, height / 2);
                    context.lineTo(center - offset, height * .78);
                    context.lineWidth = Commons.Style.space(2);
                    context.strokeStyle = strokeColor;
                    context.lineCap = "round";
                    context.lineJoin = "round";
                    context.stroke();
                }
            }
        }
        Controls.ToolTip {
            id: hint
            visible: hover.hovered && !tap.pressed
            delay: 450
            padding: Commons.Style.space(7)
            text: target.label + " (" + (target.direction < 0 ? "←" : "→") + ")"
            background: Rectangle {
                color: Commons.Color.tooltip.background
                border.color: Commons.Color.tooltip.border
                border.width: 1
                radius: Commons.Style.cornerRadius
            }
            contentItem: Panora.PlainText {
                text: hint.text
                font.pixelSize: Commons.Style.font.bodySmall
                color: Commons.Color.tooltip.text
                wrapMode: Text.NoWrap
            }
        }
    }
    SideTarget {
        id: previousTarget
        anchors.left: parent.left
        visible: root.showPrevious
        hasCard: left.ready
        available: root.previousEnabled
        direction: -1
        action: "previous"
        label: root.previousLabel
    }
    SideTarget {
        id: nextTarget
        anchors.right: parent.right
        visible: root.showNext
        hasCard: right.ready
        hasPlaceholder: root.showNextPlaceholder
        available: root.nextEnabled
        direction: 1
        action: "next"
        label: root.nextLabel
    }
}
