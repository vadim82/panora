import QtQuick
import Quickshell.Io
import "Model.js" as Model

Item {
    id: root
    property string executable: "panora"
    property string uiExecutable: ""
    property string socket: ""
    property bool previewWanted: false
    property var snapshot: Model.disconnected()
    property var detailImage: null
    property string detailKey: ""
    property var flow: null
    property string flowKey: ""
    property double flowReadAt: 0
    property int previewGeneration: 0
    property string actionError: ""
    property bool actionInFlight: false
    property string pendingAction: ""
    property string pendingInstance: ""
    property int generation: 0
    property bool ready: false
    property string requestKind: "snapshot"
    property int requestGeneration: 0
    property bool requestPreview: false
    property bool requestFlow: false
    property int requestPreviewGeneration: 0
    property string response: ""
    property bool outputInvalid: false
    property int diagnosticBytes: 0
    readonly property string currentKey: Model.imageKey(snapshot)
    readonly property var displayedImage: detailImage
    readonly property string currentFlowKey: currentKey && snapshot.flow_revision
        ? currentKey + "/" + snapshot.flow_revision : ""
    readonly property bool showFlow: previewWanted && !actionError && !!flow && flowKey === currentFlowKey
    readonly property var previousPreview: showFlow ? flow.previous : null
    readonly property var nextPreview: showFlow ? flow.next : null
    readonly property bool observing: process.running

    function validSettings() {
        return executable.length > 0 && executable.length <= 4096 && executable.indexOf("\0") < 0
            && socket.length <= 4096 && socket.indexOf("\0") < 0;
    }
    function argumentsFor(action, preview) {
        var args = action ? [root.executable, action, "--json"] : [root.executable, "shell", "snapshot"];
        if (action === "stop" && root.uiExecutable) args = [root.uiExecutable, "--quit"];
        if (action) args.push("--instance", root.pendingInstance);
        if (root.socket) args.push("--socket", root.socket);
        if (!action && preview) args.push(requestFlow ? "--cover-flow" : "--preview");
        return args;
    }
    function begin(action, preview) {
        if (process.running || !validSettings()) return false;
        requestKind = action || "snapshot";
        requestGeneration = generation;
        requestPreview = preview;
        requestFlow = preview && !!currentFlowKey;
        requestPreviewGeneration = previewGeneration;
        response = "";
        outputInvalid = false;
        diagnosticBytes = 0;
        process.command = argumentsFor(action, preview);
        process.running = true;
        deadline.restart();
        return true;
    }
    function refresh() {
        if (ready && !process.running) begin("", false);
    }
    function retryPreview() {
        detailKey = "";
        clearFlow();
        actionError = "";
        refresh();
    }
    function act(action) {
        if (actionInFlight || !Model.allowed(snapshot, action)) return;
        actionInFlight = true;
        pendingAction = action;
        pendingInstance = snapshot.instance_id;
        // Reconcile the service identity before issuing one explicit action.
        refresh();
    }
    function clearFlow() {
        flow = null;
        flowKey = "";
        flowReadAt = 0;
    }
    function reset() {
        generation++;
        pendingAction = "";
        pendingInstance = "";
        actionInFlight = false;
        detailImage = null;
        detailKey = "";
        snapshot = Model.disconnected();
        clearFlow();
        if (process.running) process.signal(9);
        else Qt.callLater(refresh);
    }
    function accept(value) {
        if (value.connected) {
            var key = Model.imageKey(value);
            if (key !== currentKey) {
                detailImage = value.image || null;
                detailKey = "";
            }
            if (value.image && value.image.preview_state !== "not_requested") {
                detailImage = value.image;
                detailKey = key;
            }
            if (!value.image) detailImage = null;
        }
        // Disconnection keeps only explicitly labeled last-known image data.
        var newFlow = value.flow;
        delete value.flow;
        snapshot = value;
        if (flowKey !== currentFlowKey) clearFlow();
        if (newFlow) {
            flow = newFlow;
            flowKey = currentFlowKey;
            flowReadAt = Date.now();
        }
    }
    function finished(code) {
        deadline.stop();
        if (requestGeneration !== generation) {
            Qt.callLater(refresh);
            return;
        }
        if (requestKind !== "snapshot") {
            actionInFlight = false;
            actionError = code === 0 && !outputInvalid && Model.actionSucceeded(response, pendingInstance, requestKind) ? "" : "actionUnconfirmed";
            if (actionError) clearFlow();
            Qt.callLater(refresh);
            return;
        }
        if (requestPreview && requestPreviewGeneration !== previewGeneration) {
            response = "";
            Qt.callLater(refresh);
            return;
        }
        var value = code === 0 && !outputInvalid ? Model.parse(response, requestFlow) : null;
        response = "";
        accept(value || Model.disconnected());
        if (pendingAction) {
            var action = pendingAction;
            pendingAction = "";
            if (snapshot.instance_id === pendingInstance && Model.allowed(snapshot, action)) {
                begin(action, false);
            } else {
                actionInFlight = false;
                actionError = "actionUnconfirmed";
            }
        } else if (!requestPreview && previewWanted && currentKey &&
                   (detailKey !== currentKey || (currentFlowKey && !actionError &&
                    (flowKey !== currentFlowKey || Date.now() - flowReadAt >= 10000)))) {
            begin("", true);
        }
    }
    onSocketChanged: if (ready) reset()
    onExecutableChanged: if (ready) reset()
    onUiExecutableChanged: if (ready) reset()
    onPreviewWantedChanged: {
        previewGeneration++;
        if (previewWanted) retryPreview();
        else {
            clearFlow();
            if (requestPreview && process.running) process.signal(9);
        }
    }
    Component.onCompleted: { ready = true; Qt.callLater(refresh); }
    Component.onDestruction: { if (process.running) process.signal(9); }

    Process {
        id: process
        stdout: SplitParser {
            // Empty marker emits arbitrary read chunks rather than buffering an
            // unbounded line. Reject before retaining more than the JSON cap.
            splitMarker: ""
            onRead: function(data) {
                if (root.outputInvalid) return;
                if (data.length > Model.snapshotLimit(root.requestFlow) - root.response.length) {
                    root.outputInvalid = true;
                    root.response = "";
                    process.signal(9);
                } else root.response += data;
            }
        }
        stderr: SplitParser {
            splitMarker: ""
            onRead: function(data) {
                // Drain diagnostics without retaining or displaying provider text.
                root.diagnosticBytes += data.length;
                if (root.diagnosticBytes > 65536) {
                    root.outputInvalid = true;
                    root.response = "";
                    process.signal(9);
                }
            }
        }
        onExited: function(code, status) { root.finished(status === 0 ? code : -1); }
    }
    Timer {
        id: deadline
        interval: root.requestKind !== "snapshot" ? 35000 : 7500
        onTriggered: {
            root.actionError = root.requestKind !== "snapshot" ? "actionUnconfirmed" : "";
            root.outputInvalid = true;
            root.response = "";
            process.signal(9);
        }
    }
    Timer {
        // This observes status only; playback scheduling stays in the service.
        interval: root.previewWanted ? 2000 : 5000
        running: root.ready
        repeat: true
        onTriggered: root.refresh()
    }
}
