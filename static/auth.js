// Favorites + Directory (client-side persistence only)
let bbsDirectory = [];
let dirflatData = [];
let dirflatSort = { col: 'fav', dir: 'desc' };
let localFavorites = [];
let isEditingFavorites = false;

document.addEventListener('DOMContentLoaded', () => {
    // Load saved sort preference or default to Fav desc
    try {
        const saved = JSON.parse(localStorage.getItem('retroterm:dirflatSort') || 'null');
        if (saved && typeof saved.col === 'string' && (saved.dir === 'asc' || saved.dir === 'desc')) {
            dirflatSort = saved;
        }
    } catch (_) {}

    // Load directory and favorites
    loadBBSDirectory().then(() => {
        localFavorites = getFavorites();
        if (Array.isArray(localFavorites) && localFavorites.length) {
            renderQuickConnect(resolveFavorites(localFavorites));
        } else {
            // If no saved favorites, seed up to 30 random entries from the directory
            seedRandomFavoritesFromDirectory();
        }
    });

    // Browse directory button opens browser modal
    const browseBtn = document.getElementById('browse-directory-btn');
    if (browseBtn) {
        browseBtn.addEventListener('click', async () => {
            if (!bbsDirectory || !bbsDirectory.length) {
                await loadBBSDirectory();
            }
            // Ensure header indicators reflect current sort
            document.getElementById('browse-directory-modal').style.display = 'block';
            hydrateDirFlatFromDirectory();
            renderDirFlat();
            updateDirflatSortIndicators();
        });
    }

    // Modal close buttons
    document.querySelectorAll('.modal-close, .modal-cancel').forEach(btn => {
        btn.addEventListener('click', (e) => {
            e.target.closest('.modal').style.display = 'none';
        });
    });

    // Edit favorites toggles inline remove buttons
    const editBtn = document.getElementById('edit-favorites-btn');
    if (editBtn) {
        editBtn.addEventListener('click', () => {
            isEditingFavorites = !isEditingFavorites;
            editBtn.textContent = isEditingFavorites ? 'Save' : 'Edit';
            const listEl = document.getElementById('bbs-directory');
            if (listEl) listEl.classList.toggle('editing', isEditingFavorites);
            const favs = resolveFavorites(getFavorites()).slice(0, 30);
            if (favs.length) renderQuickConnect(favs); else loadDefaultQuickConnect();
        });
    }

    // Directory search + table header sorting
    const searchEl = document.getElementById('directory-search');
    if (searchEl) {
        searchEl.addEventListener('input', (e) => {
            const searchTerm = e.target.value.toLowerCase();
            renderDirFlat(searchTerm);
        });
    }
    const head = document.getElementById('dirflat-table')?.querySelector('thead');
    if (head) {
        head.addEventListener('click', (e) => {
            if (e.target.tagName.toLowerCase() !== 'th') return;
            const col = e.target.getAttribute('data-col');
            if (!col) return;
            if (dirflatSort.col === col) {
                dirflatSort.dir = dirflatSort.dir === 'asc' ? 'desc' : 'asc';
            } else {
                dirflatSort.col = col;
                dirflatSort.dir = 'asc';
            }
            try { localStorage.setItem('retroterm:dirflatSort', JSON.stringify(dirflatSort)); } catch (_) {}
            renderDirFlat(document.getElementById('directory-search').value.toLowerCase());
            updateDirflatSortIndicators();
        });
    }
});

// Load BBS directory
async function loadBBSDirectory() {
    try {
        // Always prefer the server API (which reads bbs.csv when configured)
        let response = await fetch('/api/bbs-directory', { cache: 'no-cache' });
        let list = [];
        if (response.ok) {
            list = await response.json();
        }

        // Fallback to local bbs.json only if API fails or returns empty
        if (!Array.isArray(list) || list.length === 0) {
            try {
                const local = await fetch('bbs.json', { cache: 'no-cache' });
                if (local.ok) {
                    list = await local.json();
                }
            } catch (_) { /* ignore */ }
        }

        // Normalize fields to expected shape
        bbsDirectory = (list || []).map(item => ({
            id: item.id || `${(item.name||'').toLowerCase().replace(/[^a-z0-9]+/g,'-')}-${(item.host||'')}`,
            name: item.name || '',
            host: item.host || (item.address ? String(item.address).split(':')[0] : ''),
            port: item.port || (item.address ? parseInt(String(item.address).split(':')[1]||'23',10) : 23),
            protocol: (item.protocol || 'telnet').toLowerCase(),
            software: item.software || '',
            description: item.description || '',
            encoding: item.encoding || 'CP437',
            location: item.location || '',
            slug: item.slug || ''
        }));
    } catch (error) {
        console.error('Failed to load BBS directory:', error);
        bbsDirectory = [];
    }
}

// Build flat list data from directory list
function hydrateDirFlatFromDirectory() {
    dirflatData = (bbsDirectory || []).map(b => ({
        name: b.name || '',
        software: b.software || '',
        location: b.location || '',
        address: `${b.host || ''}${b.port ? ':' + b.port : ''}`,
        id: b.id,
        slug: b.slug || ''
    }));
    // Debug: Check if slugs are present
    if (dirflatData.length > 0) {
        console.log('First BBS in dirflatData:', dirflatData[0]);
        console.log('Has slug:', !!dirflatData[0].slug);
    }
}

function renderDirFlat(searchTerm = '') {
    const tbody = document.getElementById('dirflat-body');
    if (!tbody) return;
    const s = (searchTerm || '').toLowerCase();
    let rows = dirflatData.filter(r => {
        if (!s) return true;
        return (r.name || '').toLowerCase().includes(s) ||
               (r.location || '').toLowerCase().includes(s) ||
               (r.software || '').toLowerCase().includes(s) ||
               (r.address || '').toLowerCase().includes(s);
    });
    rows.sort((a, b) => {
        const col = dirflatSort.col;
        const dir = dirflatSort.dir === 'asc' ? 1 : -1;
        if (col === 'fav') {
            const favs = new Set(getFavorites());
            const av = favs.has(a.id) ? 1 : 0;
            const bv = favs.has(b.id) ? 1 : 0;
            if (av < bv) return -1 * dir;
            if (av > bv) return 1 * dir;
            // Tie-breaker by name
            const an = (a.name || '').toLowerCase();
            const bn = (b.name || '').toLowerCase();
            if (an < bn) return -1;
            if (an > bn) return 1;
            return 0;
        } else {
            const av = (a[col] || '').toString().toLowerCase();
            const bv = (b[col] || '').toString().toLowerCase();
            if (av < bv) return -1 * dir;
            if (av > bv) return 1 * dir;
            return 0;
        }
    });
    const favs = new Set(getFavorites());
    const html = rows.map((r, idx) => {
        const isFav = favs.has(r.id);
        const star = isFav ? '★' : '☆';
        const starClass = isFav ? 'dirv2-fav active' : 'dirv2-fav';
        const quickLinkUrl = r.slug ? `${window.location.origin}/${r.slug}` : '';
        // Debug first row
        if (idx === 0) {
            console.log('Rendering first BBS:', r.name, 'slug:', r.slug, 'quickLinkUrl:', quickLinkUrl);
        }
        return `
        <tr data-id="${r.id}">
            <td class="fav"><button class="${starClass}" data-id="${r.id}" title="Toggle favorite">${star}</button></td>
            <td class="name">
                <span class="bbs-name">${escapeHtml(r.name)}</span>
                ${r.slug ? `<span class="quick-link-wrapper">
                    <button class="quick-link-btn" data-slug="${escapeHtml(r.slug)}" title="Copy quick link: ${quickLinkUrl}">🔗</button>
                    <span class="quick-link-tooltip">${quickLinkUrl}</span>
                </span>` : ''}
            </td>
            <td class="location">${escapeHtml(r.location || '')}</td>
            <td class="software">${escapeHtml(r.software || '')}</td>
            <td class="addr">${escapeHtml(r.address)}</td>
        </tr>`;
    }).join('');
    tbody.innerHTML = html || '<tr><td colspan="5" class="software">No results</td></tr>';
    // Keep header sort indicators in sync
    updateDirflatSortIndicators();

    // Row click connects
    Array.from(tbody.querySelectorAll('tr')).forEach(tr => {
        tr.addEventListener('click', (e) => {
            // Ignore clicks on the star
            if (e.target && e.target.classList && e.target.classList.contains('dirv2-fav')) return;
            const id = tr.getAttribute('data-id');
            const b = (bbsDirectory || []).find(x => x.id === id);
            if (!b) return;
            connectToBBS(b);
            document.getElementById('browse-directory-modal').style.display = 'none';
        });
    });

    // Star click toggles favorite
    Array.from(tbody.querySelectorAll('.dirv2-fav')).forEach(btn => {
        btn.addEventListener('click', (e) => {
            e.stopPropagation();
            const id = btn.getAttribute('data-id');
            toggleFavoriteStar(id);
            // Re-render to update stars and disabled state
            const term = (document.getElementById('directory-search')?.value || '').toLowerCase();
            renderDirFlat(term);
        });
    });

    // Quick link button click copies URL
    Array.from(tbody.querySelectorAll('.quick-link-btn')).forEach(btn => {
        btn.addEventListener('click', async (e) => {
            e.stopPropagation();
            const slug = btn.getAttribute('data-slug');
            if (!slug) return;

            const url = `${window.location.origin}/${slug}`;

            try {
                await navigator.clipboard.writeText(url);
                btn.textContent = '✓';
                btn.title = 'Copied!';
                setTimeout(() => {
                    btn.textContent = '📋';
                    btn.title = 'Copy quick link';
                }, 2000);
            } catch (err) {
                console.error('Failed to copy:', err);
                // Fallback for older browsers
                const textarea = document.createElement('textarea');
                textarea.value = url;
                document.body.appendChild(textarea);
                textarea.select();
                document.execCommand('copy');
                document.body.removeChild(textarea);
                btn.textContent = '✓';
                setTimeout(() => {
                    btn.textContent = '📋';
                }, 2000);
            }
        });
    });
}

// Update sort indicators on table headers
function updateDirflatSortIndicators() {
    const thead = document.getElementById('dirflat-table')?.querySelector('thead');
    if (!thead) return;
    thead.querySelectorAll('th').forEach(th => {
        th.classList.remove('sorted-asc', 'sorted-desc');
    });
    const active = thead.querySelector(`th[data-col="${dirflatSort.col}"]`);
    if (active) {
        active.classList.add(dirflatSort.dir === 'asc' ? 'sorted-asc' : 'sorted-desc');
    }
}

function escapeHtml(str) {
    return (str || '').replace(/[&<>"]/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;'}[c]));
}

// Favorites (localStorage)
function getFavorites() {
    try {
        const raw = localStorage.getItem('retroterm:favorites');
        const parsed = JSON.parse(raw || '[]');
        return Array.isArray(parsed) ? parsed : [];
    } catch (_) {
        return [];
    }
}

function saveFavorites(favs) {
    try { localStorage.setItem('retroterm:favorites', JSON.stringify(favs || [])); } catch (_) {}
}

function resolveFavorites(favs) {
    const byId = new Map((bbsDirectory || []).map(b => [b.id, b]));
    return (favs || []).map(id => byId.get(id)).filter(Boolean);
}

// If no favorites, seed up to 30 random entries from the loaded directory
function seedRandomFavoritesFromDirectory() {
    const list = Array.isArray(bbsDirectory) ? bbsDirectory : [];
    if (!list.length) {
        // Fallback: if directory is empty, keep previous behavior
        loadDefaultQuickConnect();
        return;
    }
    // Pick up to 30 unique random items
    const max = Math.min(30, list.length);
    const indices = new Set();
    while (indices.size < max) {
        indices.add(Math.floor(Math.random() * list.length));
    }
    const picked = Array.from(indices).map(i => list[i]).filter(Boolean);
    const favIds = picked.map(p => p.id);
    saveFavorites(favIds);
    renderQuickConnect(resolveFavorites(favIds));
}

// Load default quick connect list
async function loadDefaultQuickConnect() {
    try {
        const response = await fetch('/api/defaultBBSList');
        const data = await response.json();
        renderQuickConnect(data.bbsList || []);
    } catch (error) {
        console.error('Failed to load default BBS list:', error);
    }
}

// Render quick connect list
function renderQuickConnect(bbsList) {
    const container = document.getElementById('bbs-directory');
    if (!container) return; // Quick Connect UI not present in this layout
    container.innerHTML = '';

    (bbsList || []).slice(0, 30).forEach(bbs => {
        const item = document.createElement('div');
        item.className = 'bbs-item compact';
        if (isEditingFavorites) {
            // Editing mode: show red X to delete, disable connect-on-click
            item.innerHTML = `
                <span class="bbs-name">${bbs.name}</span>
                <span class="bbs-details">${bbs.host}:${bbs.port}</span>
                <button class="qc-remove-x" data-id="${bbs.id}" aria-label="Remove">✖</button>
            `;
            item.onclick = (e) => { e.preventDefault(); };
        } else {
            // Normal mode: show star icon and connect on click
            item.innerHTML = `
                <span class="qc-star">★</span>
                <span class="bbs-name">${bbs.name}</span>
                <span class="bbs-details">${bbs.host}:${bbs.port}</span>
            `;
            item.onclick = () => connectToBBS(bbs);
        }
        container.appendChild(item);
    });

    // Remove handlers (red X) in edit mode
    if (isEditingFavorites) {
        container.querySelectorAll('.qc-remove-x').forEach(btn => {
            btn.addEventListener('click', (e) => {
                e.stopPropagation();
                const id = btn.getAttribute('data-id');
                removeFavoriteWithConfirm(id);
            });
        });
    }
}

// removed manage modal; edit toggles inline buttons

// Render directory grid
function renderDirectoryGrid(searchTerm = '') {
    const container = document.getElementById('full-directory-list');
    container.innerHTML = '';

    const filtered = bbsDirectory.filter(bbs => {
        if (!searchTerm) return true;
        const s = searchTerm.toLowerCase();
        const name = (bbs.name || '').toLowerCase();
        const desc = (bbs.description || '').toLowerCase();
        const host = (bbs.host || '').toLowerCase();
        return name.includes(s) || desc.includes(s) || host.includes(s);
    });

    filtered.forEach(bbs => {
        const card = document.createElement('div');
        card.className = 'dirv2-card';

        const isFavorite = (getFavorites() || []).includes(bbs.id) || bbs.is_favorite;
        const proto = (bbs.protocol || 'telnet').toUpperCase();
        const protoClass = proto === 'SSH' ? 'dirv2-chip ssh' : 'dirv2-chip protocol';

        card.innerHTML = `
            <div class="dirv2-header">
                <div class="dirv2-title">${bbs.name}</div>
                <button class=\"dirv2-fav ${isFavorite ? 'active' : ''}\" onclick=\"toggleFavorite(event, '${bbs.id}')\">★</button>
            </div>
            <div class="dirv2-meta">
                <span class="${protoClass}">${proto}</span>
                <span class="dirv2-endpoint">${bbs.host}:${bbs.port}</span>
            </div>
            ${(bbs.category || bbs.location) ? `
                <div class=\"dirv2-tags\">
                    ${bbs.category ? `<span class=\"dirv2-chip\">${bbs.category}</span>` : ''}
                    ${bbs.location ? `<span class=\"dirv2-chip\">${bbs.location}</span>` : ''}
                </div>` : ''}
            ${bbs.description ? `<div class="dirv2-desc">${bbs.description}</div>` : ''}
            <div class="dirv2-footer">
                <button class="btn-small btn-connect">Connect</button>
            </div>
        `;

        // Card click connects (except favorite star and explicit button)
        card.onclick = (e) => {
            if (!e.target.classList.contains('dirv2-fav') && !e.target.classList.contains('btn-connect')) {
                connectToBBS(bbs);
                document.getElementById('browse-directory-modal').style.display = 'none';
            }
        };

        // Explicit connect button
        const connectBtn = card.querySelector('.btn-connect');
        if (connectBtn) {
            connectBtn.addEventListener('click', (e) => {
                e.stopPropagation();
                connectToBBS(bbs);
                document.getElementById('browse-directory-modal').style.display = 'none';
            });
        }

        container.appendChild(card);
    });

    if (filtered.length === 0) {
        const empty = document.createElement('div');
        empty.className = 'dirv2-card';
        empty.innerHTML = '<div class="dirv2-title">No results</div><div class="dirv2-desc">Try a different search term.</div>';
        container.appendChild(empty);
    }
}

// Connect to BBS
function connectToBBS(bbs) {
    if (window.DEBUG) console.log('connectToBBS called with:', bbs);
    
    // Always use direct connection method since form might not exist
    if (window.bbsTerminal && window.bbsTerminal.directConnect) {
        if (window.DEBUG) console.log('Using directConnect method');
        // Prefer UI dropdown if present, else fall back to BBS-provided encoding, else CP437
        let selectedEncoding = 'CP437';
        const charsetEl = document.getElementById('charset');
        if (charsetEl && charsetEl.value) {
            selectedEncoding = charsetEl.value;
        } else if (bbs.encoding) {
            selectedEncoding = bbs.encoding;
        }

        window.bbsTerminal.directConnect(
            bbs.host,
            bbs.port,
            (bbs.protocol || 'telnet'),
            '', // username
            '', // password
            selectedEncoding
        );
        if (window.bbsTerminal && window.bbsTerminal.setCurrentBBSInfo) {
            window.bbsTerminal.setCurrentBBSInfo({ id: bbs.id, name: bbs.name, host: bbs.host, port: bbs.port, protocol: bbs.protocol });
        }
    } else {
        console.error('bbsTerminal or directConnect method not found');
        // Fallback: try to use form if it exists
        const protocolEl = document.getElementById('protocol');
        const hostEl = document.getElementById('host');
        const portEl = document.getElementById('port');
        
        if (protocolEl && hostEl && portEl) {
            protocolEl.value = bbs.protocol || 'telnet';
            hostEl.value = bbs.host;
            portEl.value = bbs.port;
            
            const charsetEl = document.getElementById('charset');
            if (bbs.encoding && charsetEl) {
                charsetEl.value = bbs.encoding;
            }
            
            if (window.bbsTerminal) {
                window.bbsTerminal.connect(true);
            }
        } else {
            alert('Unable to connect: Terminal not initialized');
        }
    }
}

// Make toggleFavorite globally accessible
// Favorites operations
function tryAddFavorite(id) {
    let favs = getFavorites();
    if (favs.includes(id)) return; // already
    if (favs.length >= 30) {
        alert('Quick Connect is full (limit 30). Remove one first.');
        return;
    }
    favs.push(id);
    saveFavorites(favs);
    const list = resolveFavorites(favs).slice(0, 30);
    if (list.length) renderQuickConnect(list); else loadDefaultQuickConnect();
}

function toggleFavoriteStar(id) {
    let favs = getFavorites();
    const idx = favs.indexOf(id);
    if (idx >= 0) {
        favs.splice(idx, 1);
    } else {
        if (favs.length >= 30) {
            alert('Quick Connect is full (limit 30). Remove one first.');
            return;
        }
        favs.push(id);
    }
    saveFavorites(favs);
    const list = resolveFavorites(favs).slice(0, 30);
    if (list.length) renderQuickConnect(list); else loadDefaultQuickConnect();
}

function removeFavoriteWithConfirm(id) {
    const b = (bbsDirectory || []).find(x => x.id === id);
    const name = b ? (b.name || `${b.host}:${b.port}`) : id;
    if (!confirm(`Remove "${name}" from Quick Connect?`)) return;
    let favs = getFavorites();
    const idx = favs.indexOf(id);
    if (idx >= 0) {
        favs.splice(idx, 1);
        saveFavorites(favs);
        const list = resolveFavorites(favs).slice(0, 30);
        if (list.length) renderQuickConnect(list); else loadDefaultQuickConnect();
        // Also refresh directory view so Add buttons enable
        const search = document.getElementById('directory-search');
        renderDirFlat((search && search.value.toLowerCase()) || '');
    }
}

// Expose for inline buttons if needed
window.removeFavoriteWithConfirm = removeFavoriteWithConfirm;
