/**
 * Imagen - Internal AI Image Generation Tool
 * Talks to the Imagen backend: session-cookie auth, generation proxy, and a
 * per-user server-side gallery. Credentials for the upstream image provider
 * live only on the backend; the browser calls the backend, never the provider.
 */

// ===== Backend API helper =====
// apiFetch sends JSON by default, throws on a non-2xx response, and — for any
// /api/* call other than login/session — tears the app down to the login
// overlay when the backend answers 401 (e.g. the session expired mid-use).
async function apiFetch(path, options = {}) {
    const opts = { credentials: 'same-origin', ...options };
    opts.headers = { ...(options.headers || {}) };

    if (opts.body !== undefined && typeof opts.body !== 'string' && !(opts.body instanceof FormData)) {
        opts.headers['Content-Type'] = 'application/json';
        opts.body = JSON.stringify(opts.body);
    }

    const res = await fetch(path, opts);

    if (res.status === 401 && path.startsWith('/api/') && path !== '/api/login' && path !== '/api/session') {
        handleSessionExpired();
        throw new Error('Your session expired. Please log in again.');
    }

    if (!res.ok) {
        let msg = `Request failed (${res.status})`;
        try {
            const body = await res.json();
            if (body && body.error) msg = body.error;
        } catch (_) { /* non-JSON error body */ }
        throw new Error(msg);
    }

    const ct = res.headers.get('Content-Type') || '';
    if (res.status === 204) return null;
    if (ct.includes('application/json')) return res.json();
    return res;
}

// ===== State Management =====
const state = {
    selectedModel: localStorage.getItem('imagen_model') || 'google/gemini-2.5-flash-image',
    imageSize: localStorage.getItem('imagen_size') || '1024x1024',
    imageQuality: localStorage.getItem('imagen_quality') || '1K',
    aspectRatio: localStorage.getItem('imagen_aspect_ratio') || '1:1',
    imageCount: parseInt(localStorage.getItem('imagen_count')) || 1,
    references: [], // Dynamic array - unlimited references (data URIs)
    images: [], // Loaded from GET /api/images
    currentImage: null,
    pendingBatches: [] // Track pending generation batches { id, prompt, count, completed, failed }
};

// ===== Model Configurations =====
const MODEL_CONFIGS = {
    'google/gemini-2.5-flash-image': {
        name: 'Gemini 2.5 Flash Image',
        supportsImageSize: true,
        supportsAspectRatio: true,
        supportsImageInput: true,
        maxReferences: 3
    },
    'google/gemini-2.5-flash-image-preview': {
        name: 'Gemini 2.5 Flash Image (Preview)',
        supportsImageSize: true,
        supportsAspectRatio: true,
        supportsImageInput: true,
        maxReferences: 3
    },
    'google/gemini-3.1-flash-image-preview': {
        name: 'Gemini 3.1 Flash Image (Preview)',
        supportsImageSize: true,
        supportsAspectRatio: true,
        supportsImageInput: true,
        maxReferences: 3
    },
    'google/gemini-3-pro-image-preview': {
        name: 'Gemini 3 Pro Image (Preview)',
        supportsImageSize: true,
        supportsAspectRatio: true,
        supportsImageInput: true,
        maxReferences: 14
    },
    'openai/gpt-5-image': {
        name: 'GPT-5 Image',
        supportsImageSize: false,
        supportsAspectRatio: true,
        supportsImageInput: true,
        maxReferences: 1
    },
    'openai/gpt-5-image-mini': {
        name: 'GPT-5 Image Mini',
        supportsImageSize: false,
        supportsAspectRatio: true,
        supportsImageInput: true,
        maxReferences: 1
    },
    'black-forest-labs/flux.2-pro': {
        name: 'Flux 2 Pro',
        supportsImageSize: false,
        supportsAspectRatio: true,
        supportsImageInput: false,
        maxReferences: 0
    },
    'black-forest-labs/flux.2-max': {
        name: 'Flux 2 Max',
        supportsImageSize: false,
        supportsAspectRatio: true,
        supportsImageInput: false,
        maxReferences: 0
    },
    'black-forest-labs/flux.2-flex': {
        name: 'Flux 2 Flex',
        supportsImageSize: false,
        supportsAspectRatio: true,
        supportsImageInput: false,
        maxReferences: 0
    },
    'black-forest-labs/flux.2-klein-4b': {
        name: 'Flux 2 Klein 4B',
        supportsImageSize: false,
        supportsAspectRatio: true,
        supportsImageInput: false,
        maxReferences: 0
    },
    'bytedance-seed/seedream-4.5': {
        name: 'Seedream 4.5',
        supportsImageSize: false,
        supportsAspectRatio: true,
        supportsImageInput: false,
        maxReferences: 0
    },
    'sourceful/riverflow-v2-fast-preview': {
        name: 'Riverflow V2 Fast',
        supportsImageSize: false,
        supportsAspectRatio: true,
        supportsImageInput: false,
        maxReferences: 0
    },
    'sourceful/riverflow-v2-standard-preview': {
        name: 'Riverflow V2 Standard',
        supportsImageSize: false,
        supportsAspectRatio: true,
        supportsImageInput: false,
        maxReferences: 0
    },
    'sourceful/riverflow-v2-max-preview': {
        name: 'Riverflow V2 Max',
        supportsImageSize: false,
        supportsAspectRatio: true,
        supportsImageInput: false,
        maxReferences: 0
    }
};

// ===== DOM Elements =====
const elements = {
    // Auth
    loginOverlay: document.getElementById('loginOverlay'),
    loginForm: document.getElementById('loginForm'),
    loginUsername: document.getElementById('loginUsername'),
    loginPassword: document.getElementById('loginPassword'),
    loginSubmit: document.getElementById('loginSubmit'),
    loginError: document.getElementById('loginError'),
    logoutBtn: document.getElementById('logoutBtn'),
    appRoot: document.getElementById('appRoot'),

    // Sidebar
    modelSelectContainer: document.getElementById('modelSelectContainer'),
    modelSelectTrigger: document.getElementById('modelSelectTrigger'),
    modelSelectValue: document.getElementById('modelSelectValue'),
    modelSelectOptions: document.getElementById('modelSelectOptions'),
    geminiOptions: document.getElementById('geminiOptions'),
    imageCount: document.getElementById('imageCount'),
    decreaseCount: document.getElementById('decreaseCount'),
    increaseCount: document.getElementById('increaseCount'),
    clearReferences: document.getElementById('clearReferences'),
    referenceSlots: document.getElementById('referenceSlots'),

    // Main Content
    promptInput: document.getElementById('promptInput'),
    charCount: document.getElementById('charCount'),
    generateBtn: document.getElementById('generateBtn'),
    gallery: document.getElementById('gallery'),
    galleryEmpty: document.getElementById('galleryEmpty'),
    clearGallery: document.getElementById('clearGallery'),

    // Modal
    imageModal: document.getElementById('imageModal'),
    modalOverlay: document.getElementById('modalOverlay'),
    modalClose: document.getElementById('modalClose'),
    modalImage: document.getElementById('modalImage'),
    modalMetadata: document.getElementById('modalMetadata'),
    useAsReference: document.getElementById('useAsReference'),
    recreateImage: document.getElementById('recreateImage'),
    downloadImage: document.getElementById('downloadImage')
};

// ===== Auth bootstrap =====
// On load, ask the backend whether the session is valid. 200 -> enter the app
// and run init(). 401 -> show the login form and do not run init().
async function bootstrap() {
    setupLoginForm();
    setupLogoutButton();

    try {
        const res = await fetch('/api/session', { credentials: 'same-origin' });
        if (res.ok) {
            await enterApp();
        } else {
            showLoginOverlay();
        }
    } catch (_) {
        showLoginOverlay();
    }
}

function showLoginOverlay() {
    elements.loginOverlay.hidden = false;
    elements.appRoot.hidden = true;
    if (elements.loginUsername) elements.loginUsername.focus();
}

async function enterApp() {
    elements.loginOverlay.hidden = true;
    elements.appRoot.hidden = false;
    await init();
}

function setupLoginForm() {
    elements.loginForm.addEventListener('submit', async (e) => {
        e.preventDefault();
        elements.loginError.textContent = '';

        const username = elements.loginUsername.value.trim();
        const password = elements.loginPassword.value;
        if (!username || !password) {
            elements.loginError.textContent = 'Enter a username and password.';
            return;
        }

        elements.loginSubmit.disabled = true;
        try {
            const res = await fetch('/api/login', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                credentials: 'same-origin',
                body: JSON.stringify({ username, password })
            });
            if (res.ok) {
                elements.loginForm.reset();
                await enterApp();
            } else if (res.status === 429) {
                elements.loginError.textContent = 'Too many attempts. Please wait a few minutes and try again.';
            } else {
                elements.loginError.textContent = 'Incorrect username or password.';
            }
        } catch (_) {
            elements.loginError.textContent = 'Could not reach the server.';
        } finally {
            elements.loginSubmit.disabled = false;
        }
    });
}

function setupLogoutButton() {
    if (!elements.logoutBtn) return;
    elements.logoutBtn.addEventListener('click', async () => {
        try {
            await fetch('/api/logout', { method: 'POST', credentials: 'same-origin' });
        } catch (_) { /* logout is best-effort */ }
        teardownToLogin();
    });
}

function resetInMemoryState() {
    state.images = [];
    state.references = [];
    state.pendingBatches = [];
    state.currentImage = null;
    if (elements.referenceSlots) renderReferenceSlots();
    if (elements.gallery) {
        elements.gallery.innerHTML = '';
        elements.gallery.appendChild(elements.galleryEmpty);
        elements.galleryEmpty.style.display = 'flex';
    }
}

function teardownToLogin() {
    closeModal();
    resetInMemoryState();
    showLoginOverlay();
}

let sessionExpiredHandled = false;
function handleSessionExpired() {
    if (sessionExpiredHandled) return;
    sessionExpiredHandled = true;
    showToast('Your session expired. Please log in again.', 'warning');
    teardownToLogin();
    setTimeout(() => { sessionExpiredHandled = false; }, 1500);
}

// ===== Initialization =====
let listenersBound = false;

async function init() {
    // Render reference slots
    renderReferenceSlots();

    // Restore saved model selection
    if (state.selectedModel) {
        const savedOption = document.querySelector(`.custom-select-option[data-value="${state.selectedModel}"]`);
        if (savedOption) {
            document.querySelectorAll('.custom-select-option').forEach(o => o.classList.remove('selected'));
            savedOption.classList.add('selected');
            elements.modelSelectValue.textContent = savedOption.textContent;
        }
    }

    // Restore saved image quality/size
    document.querySelectorAll('.btn-toggle').forEach(btn => {
        btn.classList.remove('active');
        if (btn.dataset.quality === state.imageQuality) {
            btn.classList.add('active');
        }
    });

    // Restore saved aspect ratio
    document.querySelectorAll('.btn-aspect').forEach(btn => {
        btn.classList.remove('active');
        if (btn.dataset.ratio === state.aspectRatio) {
            btn.classList.add('active');
        }
    });

    // Restore saved image count
    if (elements.imageCount) {
        elements.imageCount.value = state.imageCount;
    }

    // Set up event listeners once (init() re-runs after a re-login)
    if (!listenersBound) {
        setupEventListeners();
        listenersBound = true;
    }

    // Initialize UI state
    updateGeminiOptionsVisibility();

    // Load the gallery from the server
    await loadGallery();
}

// Map a backend image DTO to the shape the render/modal code expects.
function normalizeImage(dto) {
    return {
        id: dto.id,
        url: dto.url, // /api/images/{id}
        prompt: dto.prompt,
        model: dto.model,
        modelName: dto.modelName,
        size: dto.imageSize,
        quality: dto.imageSize,
        aspectRatio: dto.aspectRatio,
        referenceCount: dto.referenceCount || 0,
        references: [], // reference bytes are not stored server-side, only the count
        contentType: dto.contentType || 'image/png',
        createdAt: dto.createdAt || new Date().toISOString()
    };
}

async function loadGallery() {
    try {
        const list = await apiFetch('/api/images');
        state.images = (list || []).map(normalizeImage);
    } catch (error) {
        console.error('Failed to load gallery:', error);
        state.images = [];
    }
    renderGallery();
}

// ===== Event Listeners =====
function setupEventListeners() {
    // Custom dropdown - toggle
    elements.modelSelectTrigger.addEventListener('click', () => {
        elements.modelSelectContainer.classList.toggle('open');
    });

    // Custom dropdown - option selection
    document.querySelectorAll('.custom-select-option').forEach(option => {
        option.addEventListener('click', () => {
            state.selectedModel = option.dataset.value;
            localStorage.setItem('imagen_model', state.selectedModel);
            elements.modelSelectValue.textContent = option.textContent;
            document.querySelectorAll('.custom-select-option').forEach(o => o.classList.remove('selected'));
            option.classList.add('selected');
            elements.modelSelectContainer.classList.remove('open');
            updateGeminiOptionsVisibility();
        });
    });

    // Close dropdown when clicking outside
    document.addEventListener('click', (e) => {
        if (!elements.modelSelectContainer.contains(e.target)) {
            elements.modelSelectContainer.classList.remove('open');
        }
    });

    // Size toggle buttons
    document.querySelectorAll('.btn-toggle').forEach(btn => {
        btn.addEventListener('click', () => {
            document.querySelectorAll('.btn-toggle').forEach(b => b.classList.remove('active'));
            btn.classList.add('active');
            state.imageSize = btn.dataset.size;
            state.imageQuality = btn.dataset.quality;
            localStorage.setItem('imagen_size', state.imageSize);
            localStorage.setItem('imagen_quality', state.imageQuality);
        });
    });

    // Aspect ratio buttons
    document.querySelectorAll('.btn-aspect').forEach(btn => {
        btn.addEventListener('click', () => {
            document.querySelectorAll('.btn-aspect').forEach(b => b.classList.remove('active'));
            btn.classList.add('active');
            state.aspectRatio = btn.dataset.ratio;
            localStorage.setItem('imagen_aspect_ratio', state.aspectRatio);
        });
    });

    // Image count
    if (elements.decreaseCount) {
        elements.decreaseCount.addEventListener('click', () => {
            if (state.imageCount > 1) {
                state.imageCount--;
                elements.imageCount.value = state.imageCount;
                localStorage.setItem('imagen_count', state.imageCount);
            }
        });
    }

    if (elements.increaseCount) {
        elements.increaseCount.addEventListener('click', () => {
            if (state.imageCount < 8) {
                state.imageCount++;
                elements.imageCount.value = state.imageCount;
                localStorage.setItem('imagen_count', state.imageCount);
            }
        });
    }

    if (elements.imageCount) {
        elements.imageCount.addEventListener('change', (e) => {
            let val = parseInt(e.target.value);
            if (isNaN(val) || val < 1) val = 1;
            if (val > 8) val = 8;
            state.imageCount = val;
            elements.imageCount.value = val;
            localStorage.setItem('imagen_count', state.imageCount);
        });
    }

    // Reference images are handled by renderReferenceSlots()
    elements.clearReferences.addEventListener('click', clearAllReferences);

    // Drag & Drop for reference images
    setupDragAndDrop();

    // Prompt input
    elements.promptInput.addEventListener('input', () => {
        elements.charCount.textContent = `${elements.promptInput.value.length} chars`;
    });

    // Generate button
    elements.generateBtn.addEventListener('click', generateImages);

    // Clear gallery
    elements.clearGallery.addEventListener('click', async () => {
        if (!confirm('Are you sure you want to clear all generated images?')) return;
        try {
            await apiFetch('/api/images', { method: 'DELETE' });
        } catch (error) {
            showToast(error.message || 'Could not clear the gallery', 'error');
            return;
        }
        state.images = [];
        renderGallery();
        showToast('Gallery cleared', 'success');
    });

    // Modal
    elements.modalOverlay.addEventListener('click', closeModal);
    elements.modalClose.addEventListener('click', closeModal);
    elements.useAsReference.addEventListener('click', useImageAsReference);
    elements.recreateImage.addEventListener('click', recreateImage);
    elements.downloadImage.addEventListener('click', downloadCurrentImage);

    // Keyboard shortcuts
    document.addEventListener('keydown', (e) => {
        if (e.key === 'Escape') closeModal();
        if (e.key === 'Enter' && e.ctrlKey) generateImages();
    });

    // Paste images from clipboard
    document.addEventListener('paste', handlePaste);

    // Warn user before leaving if there are pending generations
    window.addEventListener('beforeunload', (e) => {
        if (state.pendingBatches.length > 0) {
            const pendingCount = state.pendingBatches.reduce((sum, batch) => {
                return sum + (batch.count - batch.completed - batch.failed);
            }, 0);
            if (pendingCount > 0) {
                e.preventDefault();
                e.returnValue = `You have ${pendingCount} image(s) still generating. If you leave, they will be lost.`;
                return e.returnValue;
            }
        }
    });
}

// ===== Paste Handler =====
function handlePaste(e) {
    // Don't intercept paste if user is typing in an input field (except prompt)
    const activeEl = document.activeElement;
    if (activeEl && activeEl.tagName === 'INPUT' && activeEl.type !== 'text') {
        return;
    }

    const items = e.clipboardData?.items;
    if (!items) return;

    let imageCount = 0;
    for (const item of items) {
        if (item.type.startsWith('image/')) {
            e.preventDefault();
            const file = item.getAsFile();
            if (file) {
                const reader = new FileReader();
                reader.onload = (event) => {
                    state.references.push(event.target.result);
                    renderReferenceSlots();
                };
                reader.readAsDataURL(file);
                imageCount++;
            }
        }
    }

    if (imageCount > 0) {
        showToast(`${imageCount} image(s) pasted as reference`, 'success');
    }
}

// ===== Drag & Drop =====
function setupDragAndDrop() {
    const dropZone = elements.referenceSlots;

    ['dragenter', 'dragover', 'dragleave', 'drop'].forEach(eventName => {
        dropZone.addEventListener(eventName, preventDefaults, false);
        document.body.addEventListener(eventName, preventDefaults, false);
    });

    function preventDefaults(e) {
        e.preventDefault();
        e.stopPropagation();
    }

    ['dragenter', 'dragover'].forEach(eventName => {
        dropZone.addEventListener(eventName, () => {
            dropZone.classList.add('drag-over');
        }, false);
    });

    ['dragleave', 'drop'].forEach(eventName => {
        dropZone.addEventListener(eventName, () => {
            dropZone.classList.remove('drag-over');
        }, false);
    });

    dropZone.addEventListener('drop', handleDrop, false);
}

function handleDrop(e) {
    const dt = e.dataTransfer;
    const files = dt.files;

    [...files].forEach(file => {
        if (file.type.startsWith('image/')) {
            const reader = new FileReader();
            reader.onload = (event) => {
                state.references.push(event.target.result);
                renderReferenceSlots();
            };
            reader.readAsDataURL(file);
        }
    });

    if (files.length > 0) {
        showToast(`${files.length} image(s) added as reference`, 'success');
    }
}

// ===== Reference Image Handling =====
function handleReferenceUpload(e) {
    const file = e.target.files[0];
    if (!file) return;

    const reader = new FileReader();
    reader.onload = (event) => {
        state.references.push(event.target.result);
        renderReferenceSlots();
    };
    reader.readAsDataURL(file);

    // Reset the input so the same file can be selected again
    e.target.value = '';
}

function renderReferenceSlots() {
    const container = document.getElementById('referenceSlots');
    container.innerHTML = '';

    // Render existing references
    state.references.forEach((ref, index) => {
        const slot = document.createElement('div');
        slot.className = 'reference-slot filled';
        slot.dataset.slot = index;
        slot.innerHTML = `
            <img src="${ref}" alt="Reference ${index + 1}">
            <button class="remove-ref" data-index="${index}">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                    <line x1="18" y1="6" x2="6" y2="18"></line>
                    <line x1="6" y1="6" x2="18" y2="18"></line>
                </svg>
            </button>
        `;
        container.appendChild(slot);
    });

    // Add "Add new" slot
    const addSlot = document.createElement('div');
    addSlot.className = 'reference-slot empty add-new';
    addSlot.innerHTML = `
        <span class="slot-label">+ Add</span>
        <input type="file" accept="image/*" class="reference-input" id="addReferenceInput">
    `;
    container.appendChild(addSlot);

    // Attach event listeners
    container.querySelectorAll('.remove-ref').forEach(btn => {
        btn.addEventListener('click', (e) => {
            e.preventDefault();
            e.stopPropagation();
            const index = parseInt(btn.dataset.index);
            removeReference(index);
        });
    });

    const addInput = container.querySelector('#addReferenceInput');
    if (addInput) {
        addInput.addEventListener('change', handleReferenceUpload);
    }
}

function removeReference(index) {
    state.references.splice(index, 1);
    renderReferenceSlots();
}

function clearAllReferences() {
    state.references = [];
    renderReferenceSlots();
    showToast('References cleared', 'success');
}

// ===== Image Generation =====
async function generateImages() {
    const prompt = elements.promptInput.value.trim();

    if (!prompt) {
        showToast('Please enter a prompt', 'warning');
        return;
    }

    const modelConfig = MODEL_CONFIGS[state.selectedModel];
    const count = state.imageCount;

    // Create a batch to track this generation request
    const batchId = Date.now() + Math.random();
    const batch = {
        id: batchId,
        prompt: prompt,
        model: state.selectedModel,
        modelName: modelConfig ? modelConfig.name : state.selectedModel,
        count: count,
        completed: 0,
        failed: 0
    };
    state.pendingBatches.push(batch);

    // Add loading placeholders without full re-render
    addLoadingPlaceholders(batch, count);
    showToast(`Generating ${count} image(s)…`, 'success');

    const payload = {
        prompt: prompt,
        model: state.selectedModel,
        imageSize: state.imageQuality,
        aspectRatio: state.aspectRatio,
        count: count,
        references: state.references.length > 0 ? [...state.references] : []
    };

    try {
        const result = await apiFetch('/api/generate', { method: 'POST', body: payload });
        const images = (result.images || []).map(normalizeImage);

        images.forEach((image) => {
            state.images.unshift(image);
            batch.completed++;
            removeOnePlaceholder(batchId);
            prependImageCard(image, 0);
        });

        batch.failed = result.failed || 0;
        clearBatchPlaceholders(batchId);

        if (images.length > 0) {
            const suffix = batch.failed > 0 ? ` (${batch.failed} failed)` : '';
            showToast(`${images.length} image(s) generated!${suffix}`, batch.failed > 0 ? 'warning' : 'success');
        } else {
            showToast('No images were generated', 'error');
        }
    } catch (error) {
        console.error('Generation failed:', error);
        clearBatchPlaceholders(batchId);
        showToast(error.message || 'Generation failed', 'error');
    } finally {
        const batchIndex = state.pendingBatches.findIndex(b => b.id === batchId);
        if (batchIndex !== -1) {
            state.pendingBatches.splice(batchIndex, 1);
        }
        maybeShowEmptyState();
    }
}

// ===== Gallery =====
function renderGallery() {
    const hasPending = state.pendingBatches.length > 0;
    const hasImages = state.images.length > 0;

    if (!hasImages && !hasPending) {
        elements.galleryEmpty.style.display = 'flex';
        elements.gallery.innerHTML = '';
        elements.gallery.appendChild(elements.galleryEmpty);
        return;
    }

    elements.gallery.innerHTML = '';

    // Render loading placeholders for pending batches at the top
    state.pendingBatches.forEach((batch) => {
        const pendingCount = batch.count - batch.completed - batch.failed;
        for (let i = 0; i < pendingCount; i++) {
            elements.gallery.appendChild(createPlaceholderElement(batch));
        }
    });

    // Render existing images
    state.images.forEach((image) => {
        elements.gallery.appendChild(createImageCardElement(image));
    });
}

// ===== Incremental Gallery Updates =====
function addLoadingPlaceholders(batch, count) {
    // Hide empty state if showing
    elements.galleryEmpty.style.display = 'none';

    for (let i = 0; i < count; i++) {
        const placeholder = createPlaceholderElement(batch);
        elements.gallery.insertBefore(placeholder, elements.gallery.firstChild);
    }
}

function createPlaceholderElement(batch) {
    const placeholder = document.createElement('div');
    placeholder.className = 'image-card loading-placeholder';
    placeholder.dataset.batchId = batch.id;
    const truncatedPrompt = batch.prompt.length > 60 ? batch.prompt.substring(0, 60) + '...' : batch.prompt;
    placeholder.innerHTML = `
        <div class="loading-placeholder-content">
            <div class="loading-spinner"></div>
            <span class="loading-placeholder-text">Generating...</span>
        </div>
        <div class="image-card-overlay" style="opacity: 1;">
            <p class="image-card-prompt">${escapeHtml(truncatedPrompt)}</p>
            <div class="image-card-meta">
                <span class="meta-tag">${escapeHtml(batch.modelName)}</span>
                <span class="meta-tag loading-tag">
                    <svg class="spin-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                        <line x1="12" y1="2" x2="12" y2="6"></line>
                        <line x1="12" y1="18" x2="12" y2="22"></line>
                        <line x1="4.93" y1="4.93" x2="7.76" y2="7.76"></line>
                        <line x1="16.24" y1="16.24" x2="19.07" y2="19.07"></line>
                        <line x1="2" y1="12" x2="6" y2="12"></line>
                        <line x1="18" y1="12" x2="22" y2="12"></line>
                        <line x1="4.93" y1="19.07" x2="7.76" y2="16.24"></line>
                        <line x1="16.24" y1="7.76" x2="19.07" y2="4.93"></line>
                    </svg>
                    Pending
                </span>
            </div>
        </div>
    `;
    return placeholder;
}

function removeOnePlaceholder(batchId) {
    const placeholder = elements.gallery.querySelector(`.loading-placeholder[data-batch-id="${batchId}"]`);
    if (placeholder) {
        placeholder.remove();
    }
    maybeShowEmptyState();
}

function clearBatchPlaceholders(batchId) {
    elements.gallery
        .querySelectorAll(`.loading-placeholder[data-batch-id="${batchId}"]`)
        .forEach(el => el.remove());
    maybeShowEmptyState();
}

function maybeShowEmptyState() {
    if (state.images.length === 0 && state.pendingBatches.length === 0) {
        elements.galleryEmpty.style.display = 'flex';
        if (!elements.gallery.contains(elements.galleryEmpty)) {
            elements.gallery.appendChild(elements.galleryEmpty);
        }
    }
}

function prependImageCard(image) {
    const card = createImageCardElement(image);

    // Insert after any remaining placeholders
    const firstNonPlaceholder = elements.gallery.querySelector('.image-card:not(.loading-placeholder)');
    if (firstNonPlaceholder) {
        elements.gallery.insertBefore(card, firstNonPlaceholder);
    } else {
        elements.gallery.appendChild(card);
    }
}

function createImageCardElement(image) {
    const card = document.createElement('div');
    card.className = 'image-card';
    card.dataset.imageId = image.id;

    const safeUrl = sanitizeImageUrl(image.url);
    const safePrompt = escapeHtml(image.prompt);

    card.innerHTML = `
        <div class="image-card-actions image-card-actions-top">
            <button class="image-card-btn image-card-download" title="Download image">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                    <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"></path>
                    <polyline points="7 10 12 15 17 10"></polyline>
                    <line x1="12" y1="15" x2="12" y2="3"></line>
                </svg>
            </button>
            <button class="image-card-btn image-card-delete" title="Delete image">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                    <polyline points="3 6 5 6 21 6"></polyline>
                    <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"></path>
                    <line x1="10" y1="11" x2="10" y2="17"></line>
                    <line x1="14" y1="11" x2="14" y2="17"></line>
                </svg>
            </button>
        </div>
        <div class="image-card-actions image-card-actions-bottom">
            <button class="image-card-btn image-card-reference" title="Use as reference">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                    <path d="M21 10c0 7-9 13-9 13s-9-6-9-13a9 9 0 0 1 18 0z"></path>
                    <circle cx="12" cy="10" r="3"></circle>
                </svg>
            </button>
            <button class="image-card-btn image-card-recreate" title="Recreate with same settings">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                    <polyline points="23 4 23 10 17 10"></polyline>
                    <polyline points="1 20 1 14 7 14"></polyline>
                    <path d="M3.51 9a9 9 0 0 1 14.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0 0 20.49 15"></path>
                </svg>
            </button>
        </div>
        <img src="${safeUrl}" alt="${safePrompt}" loading="lazy">
        <div class="image-card-overlay">
            <p class="image-card-prompt">${safePrompt}</p>
            <div class="image-card-meta">
                <span class="meta-tag">${escapeHtml(image.modelName || image.model)}</span>
                <span class="meta-tag">${escapeHtml(image.quality || image.size)}</span>
                <span class="meta-tag">${escapeHtml(image.aspectRatio)}</span>
            </div>
        </div>
    `;

    attachImageCardHandlers(card, image);
    return card;
}

function attachImageCardHandlers(card, image) {
    const imageId = image.id;

    card.querySelector('.image-card-download').addEventListener('click', (e) => {
        e.stopPropagation();
        const idx = state.images.findIndex(img => img.id === imageId);
        if (idx !== -1) downloadImageByIndex(idx);
    });

    card.querySelector('.image-card-delete').addEventListener('click', (e) => {
        e.stopPropagation();
        const idx = state.images.findIndex(img => img.id === imageId);
        if (idx !== -1) deleteImage(idx);
    });

    card.querySelector('.image-card-reference').addEventListener('click', (e) => {
        e.stopPropagation();
        const idx = state.images.findIndex(img => img.id === imageId);
        if (idx !== -1) addImageAsReference(idx);
    });

    card.querySelector('.image-card-recreate').addEventListener('click', (e) => {
        e.stopPropagation();
        const idx = state.images.findIndex(img => img.id === imageId);
        if (idx !== -1) recreateImageByIndex(idx);
    });

    card.addEventListener('click', () => {
        const idx = state.images.findIndex(img => img.id === imageId);
        if (idx !== -1) openModal(state.images[idx]);
    });
}

async function deleteImage(index) {
    const imageToDelete = state.images[index];
    if (!imageToDelete) return;

    try {
        await apiFetch('/api/images/' + encodeURIComponent(imageToDelete.id), { method: 'DELETE' });
    } catch (error) {
        showToast(error.message || 'Could not delete the image', 'error');
        return;
    }

    state.images.splice(index, 1);

    const card = elements.gallery.querySelector(`.image-card[data-image-id="${imageToDelete.id}"]`);
    if (card) {
        card.remove();
    }

    maybeShowEmptyState();
    showToast('Image deleted', 'success');
}

// Fetch an owned image's bytes and return them as a data URI, so a generated
// image can be reused as a reference (OpenRouter needs inline image data).
async function fetchImageAsDataURL(url) {
    const res = await fetch(url, { credentials: 'same-origin' });
    if (!res.ok) throw new Error(`Could not load image (${res.status})`);
    const blob = await res.blob();
    return await new Promise((resolve, reject) => {
        const reader = new FileReader();
        reader.onload = () => resolve(reader.result);
        reader.onerror = () => reject(reader.error);
        reader.readAsDataURL(blob);
    });
}

function downloadImageByIndex(index) {
    const image = state.images[index];
    if (!image) return;

    const link = document.createElement('a');
    link.href = image.url;
    const timestamp = new Date(image.createdAt).toISOString().replace(/[:.]/g, '-');
    const ext = extensionForImage(image);
    link.download = `imagen-${timestamp}.${ext}`;
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
    showToast('Image downloaded', 'success');
}

async function addImageAsReference(index) {
    const image = state.images[index];
    if (!image) return;

    try {
        const dataUrl = image.url.startsWith('data:') ? image.url : await fetchImageAsDataURL(image.url);
        state.references.push(dataUrl);
        renderReferenceSlots();
        showToast('Image added as reference', 'success');
    } catch (error) {
        console.error('Failed to add reference:', error);
        showToast('Could not add image as reference', 'error');
    }
}

function recreateImageByIndex(index) {
    const image = state.images[index];
    if (!image) return;
    applyRecreateSettings(image);
    window.scrollTo({ top: 0, behavior: 'smooth' });
}

// Restore a past image's prompt, model, size, and aspect ratio into the
// controls. Reference images are not restored: the backend records only the
// number of references used, not their bytes (see design decision 5).
function applyRecreateSettings(image) {
    elements.promptInput.value = image.prompt;
    elements.charCount.textContent = `${image.prompt.length} chars`;

    state.selectedModel = image.model;
    localStorage.setItem('imagen_model', state.selectedModel);
    const modelOption = document.querySelector(`.custom-select-option[data-value="${image.model}"]`);
    if (modelOption) {
        document.querySelectorAll('.custom-select-option').forEach(o => o.classList.remove('selected'));
        modelOption.classList.add('selected');
        elements.modelSelectValue.textContent = modelOption.textContent;
    }
    updateGeminiOptionsVisibility();

    document.querySelectorAll('.btn-toggle').forEach(btn => {
        btn.classList.remove('active');
        if (btn.dataset.quality === (image.quality || image.size)) {
            btn.classList.add('active');
            state.imageSize = btn.dataset.size;
            state.imageQuality = btn.dataset.quality;
        }
    });

    document.querySelectorAll('.btn-aspect').forEach(btn => {
        btn.classList.remove('active');
        if (btn.dataset.ratio === image.aspectRatio) {
            btn.classList.add('active');
            state.aspectRatio = image.aspectRatio;
        }
    });

    state.references = [];
    renderReferenceSlots();

    const note = image.referenceCount > 0
        ? 'Settings restored (reference images not stored — re-add them). Click Generate.'
        : 'Settings restored. Click Generate to recreate.';
    showToast(note, 'success');
}

// ===== Modal =====
function openModal(image) {
    state.currentImage = image;
    elements.modalImage.src = sanitizeImageUrl(image.url);
    elements.modalMetadata.innerHTML = `
        <p><strong>Prompt:</strong> ${escapeHtml(image.prompt)}</p>
        <p><strong>Model:</strong> ${escapeHtml(image.modelName || image.model)}</p>
        <p><strong>Size/Quality:</strong> ${escapeHtml(image.quality || image.size)}</p>
        <p><strong>Aspect Ratio:</strong> ${escapeHtml(image.aspectRatio)}</p>
        <p><strong>Created:</strong> ${escapeHtml(new Date(image.createdAt).toLocaleString())}</p>
        ${image.referenceCount > 0 ? `<p><strong>References Used:</strong> ${escapeHtml(image.referenceCount)}</p>` : ''}
    `;
    elements.imageModal.classList.add('active');
}

function closeModal() {
    elements.imageModal.classList.remove('active');
    state.currentImage = null;
}

async function useImageAsReference() {
    if (!state.currentImage) return;
    const image = state.currentImage;
    try {
        const dataUrl = image.url.startsWith('data:') ? image.url : await fetchImageAsDataURL(image.url);
        state.references.push(dataUrl);
        renderReferenceSlots();
        closeModal();
        showToast('Image added as reference', 'success');
    } catch (error) {
        console.error('Failed to add reference:', error);
        showToast('Could not add image as reference', 'error');
    }
}

function recreateImage() {
    if (!state.currentImage) return;
    const image = state.currentImage;
    closeModal();
    applyRecreateSettings(image);
    window.scrollTo({ top: 0, behavior: 'smooth' });
}

function downloadCurrentImage() {
    if (!state.currentImage) return;

    const link = document.createElement('a');
    link.href = state.currentImage.url;
    const ext = extensionForImage(state.currentImage);
    link.download = `imagen_${state.currentImage.id}.${ext}`;
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
    showToast('Download started', 'success');
}

// ===== UI Helpers =====
function updateGeminiOptionsVisibility() {
    const isGemini = state.selectedModel.includes('gemini');
    elements.geminiOptions.style.display = isGemini ? 'flex' : 'none';
}

function showToast(message, type = 'info') {
    let container = document.querySelector('.toast-container');
    if (!container) {
        container = document.createElement('div');
        container.className = 'toast-container';
        document.body.appendChild(container);
    }

    const toast = document.createElement('div');
    toast.className = `toast ${type}`;
    toast.textContent = message;
    container.appendChild(toast);

    setTimeout(() => {
        toast.style.animation = 'slideIn 0.3s ease reverse';
        setTimeout(() => toast.remove(), 300);
    }, 3000);
}

function escapeHtml(text) {
    if (text == null) return '';
    const div = document.createElement('div');
    div.textContent = String(text);
    return div.innerHTML;
}

// Pick a file extension for a download, preferring the server-provided content
// type and falling back to the URL or PNG.
function extensionForImage(image) {
    const ct = (image && image.contentType) || '';
    const map = { 'image/png': 'png', 'image/jpeg': 'jpg', 'image/webp': 'webp', 'image/gif': 'gif', 'image/svg+xml': 'svg' };
    if (map[ct]) return map[ct];
    return getImageExtension(image ? image.url : '');
}

function getImageExtension(url) {
    if (!url) return 'png';

    // Check for data URL with mime type
    if (url.startsWith('data:image/')) {
        const mimeMatch = url.match(/^data:image\/(\w+)/);
        if (mimeMatch) {
            const mime = mimeMatch[1].toLowerCase();
            if (mime === 'jpeg') return 'jpg';
            if (mime === 'png') return 'png';
            if (mime === 'gif') return 'gif';
            if (mime === 'webp') return 'webp';
            if (mime === 'svg+xml') return 'svg';
            return mime;
        }
    }

    // Check URL extension
    if (url.startsWith('http')) {
        const urlPath = url.split('?')[0];
        const ext = urlPath.split('.').pop()?.toLowerCase();
        if (['jpg', 'jpeg', 'png', 'gif', 'webp', 'svg'].includes(ext)) {
            return ext === 'jpeg' ? 'jpg' : ext;
        }
    }

    return 'png';
}

function sanitizeImageUrl(url) {
    if (!url) return '';
    // Allow inline data URIs, same-origin backend image URLs, and HTTPS URLs.
    if (url.startsWith('data:image/')) {
        return url;
    }
    if (url.startsWith('/api/images/')) {
        return url;
    }
    if (url.startsWith('https://')) {
        return url.replace(/"/g, '%22').replace(/'/g, '%27');
    }
    console.warn('Blocked unsafe image URL:', url);
    return '';
}

// ===== Global functions for inline handlers =====
window.removeReference = removeReference;

// ===== Initialize =====
document.addEventListener('DOMContentLoaded', bootstrap);
