import { createRoot } from 'react-dom/client'
import { useState, useEffect, Fragment } from 'react'
import { LineChart, Line, XAxis, YAxis, CartesianGrid, Tooltip, Legend, ResponsiveContainer, ReferenceLine } from 'recharts';
import './index.css'

// --- Components ---

// Config Modal
const ConfigModal = ({ isOpen, onClose, switches, onSave, onDelete, onSync, onUpdate }) => {
    const [selectedSwitch, setSelectedSwitch] = useState(null);
    const [currentConfig, setCurrentConfig] = useState(null);
    const [syncingId, setSyncingId] = useState(null);
    const [isSaving, setIsSaving] = useState(false);
    const [newSwitchForm, setNewSwitchForm] = useState({ name: '', ip_address: '', community: 'public', allow_port_zero: false });
    const [showConfig, setShowConfig] = useState(false);
    const [abortController, setAbortController] = useState(null);

    useEffect(() => {
        if (!isOpen) {
            setShowConfig(false);
            return;
        }

        if (selectedSwitch && isOpen) {
            const fullSwitch = switches.find(s => s.id === selectedSwitch);
            if (fullSwitch) {
                setCurrentConfig(JSON.parse(JSON.stringify(fullSwitch)));
            } else {
                setSelectedSwitch(null);
                setCurrentConfig(null);
                setShowConfig(false);
            }
        } else if (!selectedSwitch && switches.length > 0 && isOpen) {
            setSelectedSwitch(switches[0].id);
        } else if (switches.length === 0) {
            setSelectedSwitch(null);
            setCurrentConfig(null);
            setShowConfig(false);
        }
    }, [selectedSwitch, switches, isOpen]);

    if (!isOpen) return null;

    // --- Handlers ---
    const handleAddSwitch = async (e) => {
        e.preventDefault();
        setIsSaving(true);
        const controller = new AbortController();
        setAbortController(controller);

        try {
            await onSave(newSwitchForm, controller.signal);
            setNewSwitchForm({ name: '', ip_address: '', community: 'public', allow_port_zero: false });
        } catch (error) {
            if (error.name === 'AbortError') {
                console.log("Add device cancelled");
            } else {
                alert("Error adding device: " + error.message);
            }
        } finally {
            setIsSaving(false);
            setAbortController(null);
        }
    };

    const handleCancelAdd = () => {
        if (abortController) {
            abortController.abort();
        }
    };

    const handleUpdateConfig = async () => {
        setIsSaving(true);
        try {
            const payload = {
                id: currentConfig.id,
                name: currentConfig.name,
                ip_address: currentConfig.ip_address,
                community: currentConfig.community,
                detected_ports: currentConfig.detected_ports,
                allow_port_zero: currentConfig.allow_port_zero,
                enabled: currentConfig.enabled,
                config: currentConfig.config
            };
            const res = await fetch('/api/switches', {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(payload)
            });
            if (!res.ok) throw new Error("Update failed");
            alert("Configuration saved successfully.");
            onUpdate();
            setShowConfig(false);
        } catch (error) {
            alert("Error saving configuration: " + error.message);
        }
        setIsSaving(false);
    };

    const handleAddSection = () => {
        const sections = currentConfig.config.sections || [];
        let nextStart = (sections.length === 0 && currentConfig.allow_port_zero) ? 0 : 1;

        if (sections.length > 0) {
            const lastSection = currentConfig.config.sections[currentConfig.config.sections.length - 1];
            if (lastSection.port_ranges) {
                // Simple parser to find the max number in the range string
                const parts = lastSection.port_ranges.split(/[,-]/);
                let max = 0;
                parts.forEach(p => {
                    const n = parseInt(p.trim());
                    if (!isNaN(n) && n > max) max = n;
                });
                nextStart = max + 1;
            }
        }

        let nextEnd = nextStart + 23;
        const detected = parseInt(currentConfig.detected_ports);
        console.log("DEBUG: handleAddSection vars:", { nextStart, detected, initialNextEnd: nextEnd });

        if (!isNaN(detected) && detected > nextStart) {
            nextEnd = detected;
            console.log("DEBUG: nextEnd adjusted to detected:", nextEnd);
        }

        const newSection = {
            id: `sec-${Date.now()}`,
            title: "RJ45", // Default title matches type
            type: "RJ45",
            port_type: "RJ45",
            layout_type: "odd_top",
            rows: 2,
            port_ranges: `${nextStart}-${nextEnd}`,
            ports: []
        };
        setCurrentConfig(prev => ({
            ...prev,
            config: {
                sections: [...(prev.config.sections || []), newSection]
            }
        }));
    };

    const handleAddComboSection = () => {
        const sections = currentConfig.config.sections || [];
        if (sections.length === 0) return;
        const lastSection = sections[sections.length - 1];

        const newSection = {
            id: `sec-${Date.now()}`,
            title: "Combo Section",
            type: lastSection.port_type === "RJ45" ? "SFP" : "RJ45",
            port_type: lastSection.port_type === "RJ45" ? "SFP" : "RJ45",
            layout_type: lastSection.layout || lastSection.layout_type || "odd_top",
            rows: lastSection.rows || 2,
            port_ranges: lastSection.port_ranges,
            is_combo: true,
            ports: []
        };
        setCurrentConfig(prev => ({
            ...prev,
            config: {
                sections: [...(prev.config.sections || []), newSection]
            }
        }));
    };

    const handleDeleteSection = (id) => {
        if (!confirm("Are you sure you want to delete this section?")) return;
        setCurrentConfig(prev => ({
            ...prev,
            config: {
                sections: (prev.config.sections || []).filter(s => s.id !== id)
            }
        }));
    };

    const handleSectionChange = (id, field, value) => {
        setCurrentConfig(prev => ({
            ...prev,
            config: {
                sections: (prev.config.sections || []).map(s =>
                    s.id === id ? { ...s, [field]: value } : s
                )
            }
        }));
    };

    const handleSyncWrapper = async (id) => {
        setSyncingId(id);
        await onSync(id);
        setSyncingId(null);
        alert("Sync complete. Please re-open config or select device to refresh.");
    };

    const renderConfigForm = () => (
        <div className="space-y-4">
            <h3 className="text-lg font-bold text-gray-300 border-b border-gray-700 pb-2">Device Details</h3>
            <div className="grid grid-cols-2 gap-4">
                <div>
                    <label className="block text-xs text-gray-500 mb-1">Name</label>
                    <input
                        className="w-full bg-gray-700 border border-gray-600 rounded p-2 text-sm text-white"
                        value={currentConfig.name}
                        onChange={e => setCurrentConfig(p => ({ ...p, name: e.target.value }))}
                    />
                </div>
                <div>
                    <label className="block text-xs text-gray-500 mb-1">IP Address</label>
                    <input
                        className="w-full bg-gray-700 border border-gray-600 rounded p-2 text-sm text-white"
                        value={currentConfig.ip_address}
                        readOnly
                        disabled
                    />
                </div>
                <div>
                    <label className="block text-xs text-gray-500 mb-1">Community</label>
                    <input
                        className="w-full bg-gray-700 border border-gray-600 rounded p-2 text-sm text-white"
                        value={currentConfig.community}
                        onChange={e => setCurrentConfig(p => ({ ...p, community: e.target.value }))}
                    />
                </div>
                <div className="flex items-end pb-2 gap-4">
                    <label className="flex items-center space-x-2 cursor-pointer">
                        <input
                            type="checkbox"
                            className="form-checkbox h-4 w-4 text-blue-600 bg-gray-700 border-gray-600 rounded"
                            checked={currentConfig.allow_port_zero || false}
                            onChange={e => setCurrentConfig(p => ({ ...p, allow_port_zero: e.target.checked }))}
                        />
                        <span className="text-xs text-gray-300">Allow Port 0</span>
                    </label>
                    <label className="flex items-center space-x-2 cursor-pointer">
                        <input
                            type="checkbox"
                            className="form-checkbox h-4 w-4 text-blue-600 bg-gray-700 border-gray-600 rounded"
                            checked={currentConfig.enabled !== false}
                            onChange={e => setCurrentConfig(p => ({ ...p, enabled: e.target.checked }))}
                        />
                        <span className="text-xs text-gray-300">Enabled</span>
                    </label>
                </div>
            </div>
            <h3 className="text-lg font-bold text-gray-300 border-b border-gray-700 pb-2 pt-4 flex justify-between items-center">
                Port Sections
                <div className="flex items-center">
                    <button
                        onClick={handleAddSection}
                        className="bg-green-600 hover:bg-green-500 text-white px-3 py-1 rounded text-xs font-bold transition-colors mr-2"
                    >
                        + Add Section
                    </button>
                    <button
                        onClick={handleAddComboSection}
                        disabled={!currentConfig.config.sections || currentConfig.config.sections.length === 0}
                        className={`px-3 py-1 rounded text-xs font-bold transition-colors ${(!currentConfig.config.sections || currentConfig.config.sections.length === 0) ? 'bg-gray-600 text-gray-400 cursor-not-allowed' : 'bg-teal-600 hover:bg-teal-500 text-white'}`}
                    >
                        + Add Combo Section
                    </button>
                </div>
            </h3>
            {(currentConfig.config.sections || []).map((section, index) => (
                <div key={section.id} className="p-4 bg-gray-700 rounded border border-gray-600 space-y-2">
                    <div className="flex justify-between items-start mb-2"><div className="font-mono text-xs text-blue-300">Section {index + 1}</div><button onClick={() => handleDeleteSection(section.id)} className="text-red-400 text-xs hover:text-red-300">Delete</button></div>

                    {/* Range Input (Full Width) */}
                    <div className="w-full">
                        <label className="block text-xs text-gray-400 mb-1">Port Range (e.g. 1-24, 49, 51-52)</label>
                        <input
                            className="w-full bg-gray-800 border border-gray-600 rounded p-1.5 text-sm text-white"
                            value={section.port_ranges || ''}
                            onChange={e => handleSectionChange(section.id, 'port_ranges', e.target.value)}
                        />
                    </div>

                    <div className="grid grid-cols-3 gap-3">
                        <div><label className="text-xs text-gray-400">Type</label><select className="w-full bg-gray-800 p-1.5 text-sm rounded" value={section.port_type} onChange={e => handleSectionChange(section.id, 'port_type', e.target.value)}><option value="RJ45">RJ45</option><option value="SFP">SFP</option><option value="SFP+">SFP+</option><option value="SFP28">SFP28</option><option value="QSFP">QSFP</option><option value="QSFP28">QSFP28</option></select></div>
                        <div><label className="text-xs text-gray-400">Layout</label><select className="w-full bg-gray-800 p-1.5 text-sm rounded" value={section.layout} onChange={e => handleSectionChange(section.id, 'layout', e.target.value)}><option value="odd_top">Odd Top / Even Bottom</option><option value="sequential">Sequential</option></select></div>
                        <div>
                            <label className="text-xs text-gray-400">Rows (Vertical Count)</label>
                            <input
                                type="number"
                                min="1"
                                value={section.rows || 2}
                                onChange={e => handleSectionChange(section.id, 'rows', parseInt(e.target.value))}
                                className="w-full bg-gray-800 p-1.5 text-sm rounded"
                            />
                        </div>
                    </div>
                </div>
            ))}
            <div className="flex justify-end gap-3 pt-4"><button onClick={() => handleSyncWrapper(currentConfig.id)} disabled={syncingId} className="px-3 py-2 rounded border text-xs">Re-Sync</button><button onClick={handleUpdateConfig} className="bg-blue-600 px-4 py-2 rounded font-bold text-white">Save</button></div>
        </div>
    );

    const renderMainContent = () => (
        <Fragment>
            <div className="flex justify-between items-center mb-4 border-b border-gray-700 pb-2">
                <h3 className="text-md font-bold text-gray-300">{showConfig ? 'Edit Config' : 'Device List'}</h3>
                {selectedSwitch && (
                    <button onClick={() => setShowConfig(!showConfig)} className="text-sm text-blue-400">
                        {showConfig ? '← Back' : `Config →`}
                    </button>
                )}
            </div>
            {showConfig && currentConfig ? (
                renderConfigForm()
            ) : (
                <Fragment>
                    <div className="mb-8 p-4 bg-gray-900 rounded border border-gray-700">
                        <h3 className="text-md font-bold mb-2 text-gray-300">Add New Device</h3>
                        <form onSubmit={handleAddSwitch} className="grid grid-cols-2 gap-4">
                            <div><label className="text-xs text-gray-500">Name (Optional)</label><input className="w-full bg-gray-800 border-gray-600 rounded p-2 text-sm text-white" value={newSwitchForm.name} onChange={e => setNewSwitchForm({ ...newSwitchForm, name: e.target.value })} placeholder="Auto-detect if empty" /></div>
                            <div><label className="text-xs text-gray-500">IP Address</label><input className="w-full bg-gray-800 border-gray-600 rounded p-2 text-sm text-white" value={newSwitchForm.ip_address} onChange={e => setNewSwitchForm({ ...newSwitchForm, ip_address: e.target.value })} required /></div>
                            <div><label className="text-xs text-gray-500">Community</label><input className="w-full bg-gray-800 border-gray-600 rounded p-2 text-sm text-white" value={newSwitchForm.community} onChange={e => setNewSwitchForm({ ...newSwitchForm, community: e.target.value })} placeholder="public" /></div>
                            <div className="flex items-center pt-6">
                                <label className="flex items-center space-x-2 cursor-pointer">
                                    <input
                                        type="checkbox"
                                        className="form-checkbox h-4 w-4 text-blue-600 bg-gray-700 border-gray-600 rounded"
                                        checked={newSwitchForm.allow_port_zero}
                                        onChange={e => setNewSwitchForm({ ...newSwitchForm, allow_port_zero: e.target.checked })}
                                    />
                                    <span className="text-xs text-gray-300">Allow Port 0</span>
                                </label>
                            </div>
                            <div className="col-span-2 flex justify-end gap-2">
                                {isSaving && (
                                    <button type="button" onClick={handleCancelAdd} className="bg-red-600 px-4 py-2 rounded text-sm font-bold text-white hover:bg-red-500">
                                        Cancel
                                    </button>
                                )}
                                <button type="submit" disabled={isSaving} className="bg-blue-600 px-4 py-2 rounded text-sm font-bold text-white hover:bg-blue-500">
                                    {isSaving ? 'Adding...' : 'Add Device'}
                                </button>
                            </div>
                        </form>
                    </div>
                    <div className="space-y-2">{switches.map(s => (<div key={s.id} className={`flex justify-between items-center bg-gray-900 p-3 rounded border ${s.enabled === false ? 'border-gray-800 opacity-60' : 'border-gray-700'}`}><div><div className="font-bold text-white">{s.name} {s.enabled === false && <span className="text-xs text-yellow-500 ml-2">[DISABLED]</span>}</div><div className="text-xs text-gray-500">{s.ip_address}</div></div><div className="flex gap-2"><button onClick={() => { setSelectedSwitch(s.id); setCurrentConfig(JSON.parse(JSON.stringify(s))); setShowConfig(true); }} className="text-sm text-blue-400 border border-blue-900 px-3 py-1 rounded hover:bg-blue-900">Edit</button><button onClick={() => onDelete(s.id)} className="text-red-400 text-sm border border-red-900 px-3 py-1 rounded hover:bg-red-900">Delete</button></div></div>))}</div>
                </Fragment>
            )}
        </Fragment>
    );

    return (
        <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
            <div className="bg-gray-800 p-6 rounded-lg shadow-xl w-[700px] max-h-[90vh] overflow-y-auto border border-gray-700">
                <div className="flex justify-between mb-4">
                    <h2 className="text-2xl font-bold text-blue-400">Network Device Management</h2>
                    <button onClick={onClose} className="text-gray-400 hover:text-white">✕</button>
                </div>
                {renderMainContent()}
            </div>
        </div>
    );
};

// Port Component
const Port = ({ data, onClick, selectedPort, isCombo }) => {
    const COLOR_UP = "bg-[#26d07c]"; const COLOR_DOWN = "bg-[#1a1a1a]"; const BORDER_DOWN = "border-[#404040]";
    const baseWrapper = "relative flex flex-col items-center justify-center m-0.5 cursor-pointer transition-all duration-200";

    if (data.is_breakout) {
        const width = "w-[72px]"; const height = "h-[36px]";
        return (
            <div className={`${baseWrapper} ${width} ${height} bg-[#333] p-[2px] rounded-sm border border-gray-600`}>
                <div className="w-full h-full grid grid-cols-2 grid-rows-2 gap-[1px] grid-flow-col">
                    {(data.breakout_ports || []).map((sub, idx) => {
                        const isSubSelected = selectedPort?.if_name === sub.if_name;
                        const isSubUp = sub.status === 'UP';
                        return (
                            <div key={idx} className={`relative flex items-center justify-center rounded-[1px] ${isSubUp ? COLOR_UP : 'bg-gray-800'} ${isSubSelected ? 'ring-2 ring-white z-10' : 'hover:opacity-80'}`} title={sub.if_name} onClick={(e) => { e.stopPropagation(); onClick(sub); }}>
                                <span className={`text-[8px] font-bold ${isSubUp ? 'text-black' : 'text-gray-500'}`}>{idx + 1}</span>
                            </div>
                        );
                    })}
                </div>
                <div className="absolute -bottom-4 text-[9px] text-gray-400 font-mono">{data.physical_index}</div>
            </div>
        );
    }

    const isSelected = selectedPort?.if_name === data.if_name;
    const isUp = data.status === 'UP';
    let shapeStyles = ""; let innerElement = null;

    if (data.port_type && data.port_type.includes("QSFP")) {
        shapeStyles = `w-[72px] h-[36px] rounded-sm border ${isUp ? 'border-transparent' : 'border-gray-500'} ${isUp ? COLOR_UP : 'bg-[#222]'}`;
        innerElement = (<div className={`absolute bottom-0 w-[30px] h-[6px] rounded-t-sm ${isUp ? 'bg-green-800' : 'bg-blue-900'} mb-[1px]`}></div>);
    } else if (data.port_type && data.port_type.includes("RJ45")) {
        shapeStyles = `w-[34px] h-[30px] rounded-[2px] border-2 ${isUp ? 'border-green-600' : BORDER_DOWN} ${isUp ? COLOR_UP : COLOR_DOWN}`;
        innerElement = (
            <div className="w-full flex justify-center gap-[1px] mt-1 pointer-events-none">
                {[...Array(4)].map((_, i) => <div key={i} className={`w-[2px] h-[6px] shadow-sm rounded-[0.5px] ${isUp ? 'bg-green-900 opacity-40' : 'bg-[#b8860b] opacity-60'}`}></div>)}
            </div>
        );
    } else {
        shapeStyles = `w-[34px] h-[34px] rounded-[1px] border ${isUp ? 'border-transparent' : 'border-gray-500'} ${isUp ? COLOR_UP : 'bg-[#222]'}`;
        innerElement = (<div className={`absolute bottom-0 w-[14px] h-[6px] rounded-t-sm ${isUp ? 'bg-green-800' : 'bg-black'} mb-[1px]`}></div>);
    }

    return (
        <div className={baseWrapper} onClick={() => onClick(data)}>
            <div className={`relative flex items-center justify-center overflow-hidden ${shapeStyles} ${isSelected ? 'ring-2 ring-white shadow-lg z-10' : ''}`}>
                {isUp && <div className="absolute inset-0 bg-white opacity-20"></div>}
                {innerElement}
                <span className={`relative z-10 text-[10px] font-bold font-mono ${isUp ? 'text-black' : 'text-gray-400'}`}>{data.physical_index}</span>
            </div>
        </div>
    );
};

const formatSpeed = (bps) => {
    if (!bps) return '0 bps';
    if (bps >= 1000000000) return (bps / 1000000000).toFixed(2) + ' Gbps';
    if (bps >= 1000000) return (bps / 1000000).toFixed(2) + ' Mbps';
    if (bps >= 1000) return (bps / 1000).toFixed(2) + ' Kbps';
    return bps + ' bps';
};

// Detail Panel
const DetailPanel = ({ port, history }) => {
    if (!port) return <div className="p-4 bg-gray-800 rounded h-full flex items-center justify-center text-gray-500">Select a port</div>;
    const dom = port.dom || {};
    const hasDom = dom.temperature != null || dom.voltage != null || dom.tx_power != null || dom.rx_power != null;
    const isTrunk = port.mode === 'trunk';

    return (
        <div className="p-4 bg-gray-800 rounded h-full border border-gray-700 flex flex-col gap-4">
            <div className="flex justify-between items-start gap-4">
                <div className="w-1/2 pr-4">
                    <h3 className="text-xl font-bold mb-4 flex items-center"><span className="mr-2">Port {port.physical_index}</span><span className={`text-xs px-2 py-1 rounded font-bold ${port.status === 'UP' ? 'bg-green-600 text-white' : 'bg-gray-600 text-gray-300'}`}>{port.status}</span></h3>
                    <div className="grid grid-cols-2 gap-y-3 gap-x-4 text-sm">
                        {/* Row 1 */}
                        <div><p className="text-xs text-gray-400">Interface</p><p className="font-mono break-all">{port.if_name}</p></div>
                        <div><p className="text-xs text-gray-400">Type</p><p>{port.port_type}</p></div>

                        {/* Row 2 */}
                        <div><p className="text-xs text-gray-400">Description</p><p className="font-mono text-gray-300 truncate" title={port.if_desc}>{port.if_desc || '-'}</p></div>
                        <div><p className="text-xs text-gray-400">Out Rate</p><p className="font-mono text-blue-400 font-bold">↑ {formatSpeed(port.out_rate)}</p></div>

                        {/* Row 3 */}
                        <div><p className="text-xs text-gray-400">Speed</p><p className="font-mono">{formatSpeed(port.speed)}</p></div>
                        <div><p className="text-xs text-gray-400">In Rate</p><p className="font-mono text-green-400 font-bold">↓ {formatSpeed(port.in_rate)}</p></div>

                        {/* Row 4 */}
                        <div className="col-span-2">
                            <p className="text-xs text-gray-400">{isTrunk ? "Trunk VLANs" : "Access VLAN"}</p>
                            <div className="font-mono break-all text-xs">
                                {isTrunk ? (
                                    <>
                                        <span className="text-orange-400 font-bold">Native: {port.vlan_id || '-'}</span><br />
                                        <span className="text-gray-300 text-[10px] leading-tight">Allowed: {port.allowed_vlans || 'None'}</span>
                                    </>
                                ) : (
                                    <span className="text-green-400 font-bold text-lg">VLAN {port.vlan_id > 0 ? port.vlan_id : '-'}</span>
                                )}
                            </div>
                        </div>
                    </div>
                </div>

                {/* Right Column: Graph */}
                <div className="w-1/2 h-48 bg-gray-900 rounded p-2 border border-gray-600 flex flex-col">
                    <div className="text-xs text-gray-400 mb-1 text-center flex-shrink-0">Traffic History (Last 3 min)</div>
                    <div className="flex-1 min-h-0">
                        <ResponsiveContainer width="100%" height="100%">
                            <LineChart data={history}>
                                <CartesianGrid strokeDasharray="3 3" stroke="#333" />
                                <XAxis dataKey="time" hide />
                                <YAxis domain={[0, 'auto']} tickFormatter={(val) => formatSpeed(val)} width={65} style={{ fontSize: '10px', fill: '#999' }} interval="preserveStartEnd" />
                                <Tooltip contentStyle={{ backgroundColor: '#1f2937', borderColor: '#374151', color: '#fff' }} formatter={(val) => [formatSpeed(val), '']} labelStyle={{ color: '#9ca3af' }} />
                                <Legend verticalAlign="top" height={20} iconSize={8} wrapperStyle={{ fontSize: '10px' }} />
                                <ReferenceLine y={0} stroke="#999" strokeWidth={1} strokeDasharray="3 3" />
                                <Line type="monotone" dataKey="in" name="In" stroke="#4ade80" strokeWidth={2} dot={false} isAnimationActive={false} />
                                <Line type="monotone" dataKey="out" name="Out" stroke="#60a5fa" strokeWidth={2} dot={false} isAnimationActive={false} />
                            </LineChart>
                        </ResponsiveContainer>
                    </div>
                </div>
            </div>

            {/* DOM Info */}
            {hasDom && (
                <div className="mt-2 pt-2 border-t border-gray-700">
                    <h4 className="text-sm font-bold text-blue-300 mb-2">DOM/DDM Info</h4>
                    <div className="grid grid-cols-2 md:grid-cols-5 gap-4 text-sm">
                        {dom.temperature != null && <div><p className="text-gray-400 text-xs">Temp</p><p>{dom.temperature.toFixed(1)} °C</p></div>}
                        {dom.voltage != null && <div><p className="text-gray-400 text-xs">Volt</p><p>{dom.voltage.toFixed(2)} V</p></div>}
                        {dom.tx_power != null && <div><p className="text-gray-400 text-xs">Tx</p><p>{dom.tx_power.toFixed(2)} dBm</p></div>}
                        {dom.rx_power != null && <div><p className="text-gray-400 text-xs">Rx</p><p>{dom.rx_power.toFixed(2)} dBm</p></div>}
                        {dom.bias_current != null && <div><p className="text-gray-400 text-xs">Bias</p><p>{dom.bias_current.toFixed(2)} mA</p></div>}
                    </div>
                </div>
            )}
        </div>
    );
};

const PortSectionDisplay = ({ section, onClick, selectedPort }) => {
    const portsArray = section.ports || [];
    const sortedPorts = [...portsArray].sort((a, b) => a.physical_index - b.physical_index);
    let rowsArr = [];

    const rowCount = section.rows || 2;
    const itemsPerRow = Math.ceil(sortedPorts.length / rowCount);
    const layout = section.layout || section.layout_type || 'odd_top';

    if (layout === 'odd_top') {
        const top = sortedPorts.filter(p => p.physical_index % 2 !== 0);
        const bottom = sortedPorts.filter(p => p.physical_index % 2 === 0);

        // If rows == 2, just put top then bottom
        if (rowCount === 2) {
            rowsArr.push(top);
            rowsArr.push(bottom);
        } else {
            for (let i = 0; i < rowCount; i++) {
                const start = i * itemsPerRow;
                rowsArr.push(sortedPorts.slice(start, start + itemsPerRow));
            }
        }
    } else {
        // Sequential: Split into N rows
        for (let i = 0; i < rowCount; i++) {
            const start = i * itemsPerRow;
            const end = start + itemsPerRow;
            rowsArr.push(sortedPorts.slice(start, end));
        }
    }

    return (
        <div className="flex-shrink-0">
            <h4 className="text-sm font-mono text-gray-500 mb-2 text-center border-b border-gray-700 pb-1">{section.port_type}</h4>
            <div className={`bg-gray-800 rounded p-2 flex flex-col gap-1.5`}>
                {rowsArr.map((row, rowIndex) => (
                    <div key={rowIndex} className="flex gap-1.5 justify-start">
                        {row.map(port => (<Port key={port.if_name} data={port} onClick={onClick} selectedPort={selectedPort} />))}
                    </div>
                ))}
            </div>
        </div>
    );
};

const UsageLegend = ({ ports }) => {
    const total = ports.length;
    const up = ports.filter(p => p.status === 'UP').length;
    const down = total - up;
    const usagePercent = total > 0 ? ((up / total) * 100).toFixed(1) : 0;

    return (
        <div className="flex flex-col gap-2 bg-gray-900 p-3 rounded border border-gray-700">
            <div className="flex gap-6 text-sm text-gray-400 items-center">
                <div className="flex gap-4 border-r border-gray-700 pr-4">
                    <div><span className="font-bold text-white">{total}</span> Total</div>
                    <div><span className="font-bold text-green-400">{up}</span> Up</div>
                    <div><span className="font-bold text-gray-500">{down}</span> Down</div>
                    <div><span className="font-bold text-blue-400">{usagePercent}%</span> Usage</div>
                </div>
                <div className="flex gap-4 items-center">
                    <div className="flex items-center gap-2"><div className="w-3 h-3 bg-[#26d07c] rounded-sm"></div><span>UP</span></div>
                    <div className="flex items-center gap-2"><div className="w-3 h-3 bg-[#1a1a1a] border border-[#404040] rounded-sm"></div><span>DOWN</span></div>
                </div>
            </div>
            <div className="text-[10px] text-gray-500 border-t border-gray-800 pt-1 mt-1">
                <strong>Criteria:</strong> Status is determined by SNMP <code>ifOperStatus</code> (OID .1.3.6.1.2.1.2.2.1.8). UP = 1, DOWN != 1.
            </div>
        </div>
    );
};

// Login Component
const Login = ({ onLogin }) => {
    const [username, setUsername] = useState("");
    const [password, setPassword] = useState("");
    const [error, setError] = useState("");

    const handleSubmit = async (e) => {
        e.preventDefault();
        try {
            const res = await fetch('/api/login', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ username, password })
            });
            if (!res.ok) throw new Error("Invalid credentials");
            const data = await res.json();
            onLogin(data);
        } catch (err) {
            setError(err.message);
        }
    };

    return (
        <div className="min-h-screen flex items-center justify-center bg-gray-900">
            <div className="bg-gray-800 p-8 rounded-lg shadow-xl w-96 border border-gray-700">
                <h2 className="text-2xl font-bold text-white mb-6 text-center">Login</h2>
                {error && <div className="bg-red-900 text-red-200 p-2 rounded mb-4 text-sm text-center">{error}</div>}
                <form onSubmit={handleSubmit} className="space-y-4">
                    <div>
                        <label className="block text-gray-400 text-sm mb-1">Username</label>
                        <input className="w-full bg-gray-700 border border-gray-600 rounded p-2 text-white focus:outline-none focus:border-blue-500" value={username} onChange={e => setUsername(e.target.value)} />
                    </div>
                    <div>
                        <label className="block text-gray-400 text-sm mb-1">Password</label>
                        <input type="password" className="w-full bg-gray-700 border border-gray-600 rounded p-2 text-white focus:outline-none focus:border-blue-500" value={password} onChange={e => setPassword(e.target.value)} />
                    </div>
                    <button type="submit" className="w-full bg-blue-600 hover:bg-blue-500 text-white font-bold py-2 rounded transition-colors">Sign In</button>
                </form>
            </div>
        </div>
    );
};

// User Management Component
const UserManagement = ({ isOpen, onClose }) => {
    const [users, setUsers] = useState([]);
    const [newUser, setNewUser] = useState({ username: '', password: '', role: 'user' });

    useEffect(() => {
        if (isOpen) fetchUsers();
    }, [isOpen]);

    const fetchUsers = () => {
        fetch('/api/users').then(res => res.json()).then(data => setUsers(data || []));
    };

    const handleAddUser = async (e) => {
        e.preventDefault();
        await fetch('/api/users', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(newUser)
        });
        setNewUser({ username: '', password: '', role: 'user' });
        fetchUsers();
    };

    const handleDeleteUser = async (id) => {
        if (!confirm("Delete user?")) return;
        await fetch(`/api/users?id=${id}`, { method: 'DELETE' });
        fetchUsers();
    };

    if (!isOpen) return null;

    return (
        <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
            <div className="bg-gray-800 p-6 rounded-lg shadow-xl w-[600px] max-h-[90vh] overflow-y-auto border border-gray-700">
                <div className="flex justify-between mb-4"><h2 className="text-xl font-bold text-white">User Management</h2><button onClick={onClose} className="text-gray-400 hover:text-white">✕</button></div>

                <div className="mb-6 bg-gray-900 p-4 rounded border border-gray-700">
                    <h3 className="text-sm font-bold text-gray-300 mb-2">Add User</h3>
                    <form onSubmit={handleAddUser} className="grid grid-cols-3 gap-2">
                        <input className="bg-gray-800 border border-gray-600 rounded p-1.5 text-sm text-white" placeholder="Username" value={newUser.username} onChange={e => setNewUser({ ...newUser, username: e.target.value })} required />
                        <input className="bg-gray-800 border border-gray-600 rounded p-1.5 text-sm text-white" placeholder="Password" type="password" value={newUser.password} onChange={e => setNewUser({ ...newUser, password: e.target.value })} required />
                        <div className="flex gap-2">
                            <select className="bg-gray-800 border border-gray-600 rounded p-1.5 text-sm text-white" value={newUser.role} onChange={e => setNewUser({ ...newUser, role: e.target.value })}>
                                <option value="user">User</option>
                                <option value="admin">Admin</option>
                            </select>
                            <button type="submit" className="bg-green-600 hover:bg-green-500 text-white px-3 rounded text-sm font-bold">Add</button>
                        </div>
                    </form>
                </div>

                <div className="space-y-2">
                    {users.map(u => (
                        <div key={u.id} className="flex justify-between items-center bg-gray-900 p-3 rounded border border-gray-700">
                            <div><span className="font-bold text-white">{u.username}</span> <span className="text-xs text-gray-500 ml-2">({u.role})</span></div>
                            <button onClick={() => handleDeleteUser(u.id)} className="text-red-400 text-xs hover:text-red-300 border border-red-900 px-2 py-1 rounded">Delete</button>
                        </div>
                    ))}
                </div>
            </div>
        </div>
    );
};

// Change Password Component
const ChangePassword = ({ isOpen, onClose, forceChange, onPasswordChanged }) => {
    const [oldPassword, setOldPassword] = useState("");
    const [newPassword, setNewPassword] = useState("");
    const [confirmPassword, setConfirmPassword] = useState("");
    const [error, setError] = useState("");
    const [success, setSuccess] = useState("");

    const handleSubmit = async (e) => {
        e.preventDefault();
        if (newPassword !== confirmPassword) { setError("New passwords do not match"); return; }
        try {
            const res = await fetch('/api/change-password', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ old_password: oldPassword, new_password: newPassword })
            });
            if (!res.ok) {
                const text = await res.text();
                throw new Error(text || "Failed to change password");
            }
            setSuccess("Password changed successfully!");
            setError("");
            setOldPassword(""); setNewPassword(""); setConfirmPassword("");
            setTimeout(() => {
                setSuccess("");
                onPasswordChanged();
                if (!forceChange) onClose();
            }, 1500);
        } catch (err) {
            setError(err.message);
            setSuccess("");
        }
    };

    if (!isOpen && !forceChange) return null;

    return (
        <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
            <div className="bg-gray-800 p-6 rounded-lg shadow-xl w-96 border border-gray-700">
                <div className="flex justify-between mb-4">
                    <h2 className="text-xl font-bold text-white">{forceChange ? "Change Password Required" : "Change Password"}</h2>
                    {!forceChange && <button onClick={onClose} className="text-gray-400 hover:text-white">✕</button>}
                </div>
                {error && <div className="bg-red-900 text-red-200 p-2 rounded mb-4 text-sm text-center">{error}</div>}
                {success && <div className="bg-green-900 text-green-200 p-2 rounded mb-4 text-sm text-center">{success}</div>}
                <form onSubmit={handleSubmit} className="space-y-4">
                    <div>
                        <label className="block text-gray-400 text-sm mb-1">Old Password</label>
                        <input type="password" className="w-full bg-gray-700 border border-gray-600 rounded p-2 text-white focus:outline-none focus:border-blue-500" value={oldPassword} onChange={e => setOldPassword(e.target.value)} required />
                    </div>
                    <div>
                        <label className="block text-gray-400 text-sm mb-1">New Password</label>
                        <input type="password" className="w-full bg-gray-700 border border-gray-600 rounded p-2 text-white focus:outline-none focus:border-blue-500" value={newPassword} onChange={e => setNewPassword(e.target.value)} required />
                    </div>
                    <div>
                        <label className="block text-gray-400 text-sm mb-1">Confirm New Password</label>
                        <input type="password" className="w-full bg-gray-700 border border-gray-600 rounded p-2 text-white focus:outline-none focus:border-blue-500" value={confirmPassword} onChange={e => setConfirmPassword(e.target.value)} required />
                    </div>
                    <button type="submit" className="w-full bg-blue-600 hover:bg-blue-500 text-white font-bold py-2 rounded transition-colors">Change Password</button>
                </form>
            </div>
        </div>
    );
};

const App = () => {
    const [user, setUser] = useState(null);
    const [switches, setSwitches] = useState([]);
    const [selectedSwitchId, setSelectedSwitchId] = useState(null);
    const [portsBySection, setPortsBySection] = useState([]);
    const [systemInfo, setSystemInfo] = useState(null);
    const [selectedPort, setSelectedPort] = useState(null);
    const [isConfigOpen, setIsConfigOpen] = useState(false);
    const [isUserMgmtOpen, setIsUserMgmtOpen] = useState(false);
    const [isChangePasswordOpen, setIsChangePasswordOpen] = useState(false);
    const [trafficHistory, setTrafficHistory] = useState({});
    const [isAlwaysPolling, setIsAlwaysPolling] = useState(false);
    const [loading, setLoading] = useState(true);

    useEffect(() => {
        fetch('/api/me').then(res => {
            if (res.ok) return res.json();
            throw new Error("Not logged in");
        }).then(data => {
            setUser(data);
            setLoading(false);
        }).catch(() => {
            setUser(null);
            setLoading(false);
        });
    }, []);

    const fetchSwitches = () => {
        fetch('/api/switches').then(res => res.json()).then(data => {
            setSwitches(data || []);
            if (data && data.length > 0 && !data.find(s => s.id === selectedSwitchId)) {
                setSelectedSwitchId(data[0].id);
            } else if (data && data.length > 0 && !selectedSwitchId) {
                setSelectedSwitchId(data[0].id);
            } else if (!data || data.length === 0) {
                setSelectedSwitchId(null); setPortsBySection([]); setSystemInfo(null);
            }
        });
    };

    useEffect(() => {
        if (user && !user.password_change_required) fetchSwitches();
    }, [user]);

    useEffect(() => {
        if (!selectedSwitchId || !user || user.password_change_required) { setPortsBySection([]); setSystemInfo(null); return; }
        let isMounted = true;
        const updateStatus = async () => {
            if (!isMounted) return;
            if (document.hidden && !isAlwaysPolling) {
                timeoutId = setTimeout(updateStatus, 3000);
                return;
            }

            try {
                const res = await fetch(`/api/switches/status?id=${selectedSwitchId}`);
                if (!isMounted) return;
                const data = await res.json();
                if (data.sections) {
                    setPortsBySection(data.sections || []);
                    setSystemInfo(data.system || null);
                    setTrafficHistory(prev => {
                        const newHistory = { ...prev };
                        const now = new Date().toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' });
                        const flatPorts = [];
                        data.sections.forEach(sec => {
                            sec.ports?.forEach(p => {
                                flatPorts.push(p);
                                if (p.is_breakout && p.breakout_ports) flatPorts.push(...p.breakout_ports);
                            });
                        });
                        flatPorts.forEach(p => {
                            const currentList = newHistory[p.if_name] ? [...newHistory[p.if_name]] : [];
                            currentList.push({ time: now, in: p.in_rate, out: p.out_rate });
                            if (currentList.length > 60) currentList.shift();
                            newHistory[p.if_name] = currentList;
                        });
                        return newHistory;
                    });
                } else {
                    setPortsBySection(data || []); setSystemInfo(null);
                }
            } catch (error) {
                console.error("Polling error:", error);
            } finally {
                if (isMounted) {
                    timeoutId = setTimeout(updateStatus, 3000);
                }
            }
        };

        let timeoutId = setTimeout(updateStatus, 0);
        return () => {
            isMounted = false;
            clearTimeout(timeoutId);
        };
    }, [selectedSwitchId, switches, isAlwaysPolling, user]);

    const getSelectedPortData = () => {
        if (!selectedPort) return null;
        for (const sec of portsBySection) {
            const portsToSearch = sec.ports || [];
            for (const p of portsToSearch) {
                if (p.if_name === selectedPort.if_name) return p;
                if (p.is_breakout && p.breakout_ports) {
                    const sub = p.breakout_ports.find(sp => sp.if_name === selectedPort.if_name);
                    if (sub) return sub;
                }
            }
        }
        return selectedPort;
    };

    const handleSaveSwitch = async (newSwitch, signal) => {
        const res = await fetch('/api/switches', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(newSwitch),
            signal: signal
        });
        if (!res.ok) throw new Error("Failed to add switch");
        fetchSwitches();
    };
    const handleDeleteSwitch = async (id) => {
        if (!confirm("Are you sure?")) return;
        await fetch(`/api/switches?id=${id}`, { method: 'DELETE' });
        fetchSwitches();
    };
    const handleSyncSwitch = async (id) => {
        try {
            const res = await fetch(`/api/switches/sync?id=${id}`, { method: 'POST' });
            if (!res.ok) throw new Error("Sync failed");
            fetchSwitches();
            if (selectedSwitchId === id) setSelectedPort(null);
            alert("Sync complete!");
        } catch (e) { alert("Sync failed: " + e.message); }
    };

    const handleLogout = async () => {
        await fetch('/api/logout', { method: 'POST' });
        setUser(null);
    };

    if (loading) return <div className="min-h-screen bg-gray-900 flex items-center justify-center text-white">Loading...</div>;
    if (!user) return <Login onLogin={setUser} />;

    if (user.password_change_required) {
        return (
            <div className="min-h-screen bg-gray-900 flex items-center justify-center">
                <ChangePassword isOpen={true} forceChange={true} onPasswordChanged={() => setUser({ ...user, password_change_required: false })} />
            </div>
        );
    }

    return (
        <div className="min-h-screen p-8 flex flex-col gap-6">
            <header className="flex flex-col gap-4 border-b border-gray-700 pb-4">
                <div className="flex justify-between items-center">
                    <div className="flex items-center gap-3">
                        <div className="text-3xl">🔌</div>
                        <h1 className="text-3xl font-bold text-white">{systemInfo?.name || (switches.find(s => s.id === selectedSwitchId)?.name) || "Network Device Visualizer"}</h1>
                    </div>
                    <div className="flex gap-4 items-center">
                        {/* Toggle Switch */}
                        <label className="flex items-center cursor-pointer mr-4">
                            <div className="relative">
                                <input type="checkbox" className="sr-only" checked={isAlwaysPolling} onChange={e => setIsAlwaysPolling(e.target.checked)} />
                                <div className={`block w-10 h-6 rounded-full ${isAlwaysPolling ? 'bg-green-600' : 'bg-gray-600'}`}></div>
                                <div className={`dot absolute left-1 top-1 bg-white w-4 h-4 rounded-full transition ${isAlwaysPolling ? 'transform translate-x-4' : ''}`}></div>
                            </div>
                            <div className="ml-3 text-gray-400 text-xs font-medium">Always Poll</div>
                        </label>

                        <select
                            className="bg-gray-800 border border-gray-600 text-white p-2 rounded w-64"
                            value={selectedSwitchId || ''}
                            onChange={(e) => setSelectedSwitchId(Number(e.target.value))}
                            disabled={switches.length === 0}
                        >
                            {switches.length === 0 && <option value="">No devices</option>}
                            {switches.map(s => (
                                <option key={s.id} value={s.id}>
                                    {s.name} ({s.ip_address})
                                </option>
                            ))}
                        </select>
                        {user.role === 'admin' && (
                            <button onClick={() => setIsUserMgmtOpen(true)} className="w-32 flex justify-center items-center bg-purple-700 hover:bg-purple-600 text-white px-4 py-2 rounded border border-purple-500 transition-colors">Users</button>
                        )}
                        <button onClick={() => setIsChangePasswordOpen(true)} className="w-32 flex justify-center items-center bg-blue-700 hover:bg-blue-600 text-white px-4 py-2 rounded border border-blue-500 transition-colors">Password</button>
                        {user.role === 'admin' && (
                            <button onClick={() => setIsConfigOpen(true)} className="w-32 flex justify-center items-center bg-gray-700 hover:bg-gray-600 text-white px-4 py-2 rounded border border-gray-600 transition-colors">⚙ Config</button>
                        )}
                        <button onClick={handleLogout} className="w-32 flex justify-center items-center bg-red-700 hover:bg-red-600 text-white px-4 py-2 rounded border border-red-500 transition-colors">Logout</button>
                    </div>
                </div>
                {systemInfo && (
                    <div className="w-full grid grid-cols-[auto_1fr] gap-x-2 gap-y-1 text-sm text-gray-400 mt-1 bg-gray-800 p-2 rounded border border-gray-700">
                        <div className="font-bold text-gray-500 text-right">Uptime:</div><div>{systemInfo.uptime}</div>
                        <div className="font-bold text-gray-500 text-right">Descr:</div><div>{systemInfo.descr || "-"}</div>
                        <div className="font-bold text-gray-500 text-right">Location:</div><div>{systemInfo.location || "-"}</div>
                    </div>
                )}
            </header>
            <main className="flex-grow flex flex-col gap-8">
                <div className="w-full bg-black rounded-lg p-6 shadow-2xl border border-gray-800 overflow-x-auto">
                    <div className="min-w-[800px]"><div className="mb-2 text-gray-500 text-sm font-mono">Front Panel View</div>
                        <div className="bg-gray-900 border-4 border-gray-700 rounded p-4 flex flex-row gap-6 min-h-[200px] items-center overflow-x-auto">
                            {portsBySection.length > 0 ? (
                                (() => {
                                    const elements = [];
                                    for (let i = 0; i < portsBySection.length; i++) {
                                        const current = portsBySection[i];
                                        const next = portsBySection[i + 1];
                                        if (next && next.is_combo) {
                                            elements.push(
                                                <div key={`${current.id}-group`} className="flex gap-1.5 p-2 border-2 border-indigo-500 rounded relative bg-gray-800/50">
                                                    <div className="absolute -top-2.5 left-2 bg-indigo-500 text-white text-[10px] px-1.5 rounded">Combo Group</div>
                                                    <PortSectionDisplay section={current} onClick={setSelectedPort} selectedPort={selectedPort} />
                                                    <PortSectionDisplay section={next} onClick={setSelectedPort} selectedPort={selectedPort} />
                                                </div>
                                            );
                                            i++;
                                        } else {
                                            elements.push(<PortSectionDisplay key={current.id} section={current} onClick={setSelectedPort} selectedPort={selectedPort} />);
                                        }
                                    }
                                    return elements;
                                })()
                            ) : (
                                <div className="text-gray-600 text-center w-full">
                                    {switches.length === 0 ? "Add a device in Config." : "Loading / No ports."}
                                </div>
                            )}
                        </div>
                        <div className="mt-2">
                            <UsageLegend ports={portsBySection.flatMap(s => s.ports || []).flatMap(p => p.is_breakout ? (p.breakout_ports || []) : [p])} />
                        </div>
                    </div>
                </div>
                <div className="w-full"><DetailPanel port={getSelectedPortData()} history={selectedPort ? (trafficHistory[selectedPort.if_name] || []) : []} /></div>
            </main>
            <ConfigModal isOpen={isConfigOpen} onClose={() => { setIsConfigOpen(false); fetchSwitches(); }} switches={switches} onSave={handleSaveSwitch} onDelete={handleDeleteSwitch} onSync={handleSyncSwitch} onUpdate={fetchSwitches} />
            <UserManagement isOpen={isUserMgmtOpen} onClose={() => setIsUserMgmtOpen(false)} />
            <ChangePassword isOpen={isChangePasswordOpen} onClose={() => setIsChangePasswordOpen(false)} forceChange={false} onPasswordChanged={() => { }} />
        </div>
    );
};

createRoot(document.getElementById('root')).render(<App />)