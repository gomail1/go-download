/**
 * CSRF 全局防护脚本
 *
 * 服务端在各页面 <head> 中输出：
 *   <meta name="csrf-token" content="...">
 *
 * 本脚本读取该令牌，并自动为本站（同源）的非安全方法请求
 * （POST / PUT / PATCH / DELETE）附加 X-CSRF-Token 请求头，
 * 覆盖 fetch 与 XMLHttpRequest 两种调用方式。
 *
 * 这样业务 JS 无需逐处改动即可获得 CSRF 防护。
 */
(function () {
    'use strict';

    var TOKEN_META = 'meta[name="csrf-token"]';
    var SAFE_METHODS = /^(GET|HEAD|OPTIONS|TRACE)$/i;

    function getToken() {
        var meta = document.querySelector(TOKEN_META);
        return meta ? (meta.getAttribute('content') || '') : '';
    }

    // 仅对同源请求附加令牌，避免将令牌泄漏到第三方站点
    function isSameOrigin(url) {
        try {
            // 相对路径会基于当前页面地址解析
            return new URL(url, window.location.origin).origin === window.location.origin;
        } catch (e) {
            return false;
        }
    }

    function needsToken(method, url) {
        return !SAFE_METHODS.test(method || 'GET') && isSameOrigin(url || '');
    }

    // ---- patch window.fetch ----
    if (typeof window.fetch === 'function') {
        var originalFetch = window.fetch;
        window.fetch = function (input, init) {
            try {
                var isRequestObj = typeof Request !== 'undefined' && input instanceof Request;
                var url = typeof input === 'string' ? input : (isRequestObj ? input.url : '');
                var method = (init && init.method) || (isRequestObj ? input.method : 'GET');

                if (needsToken(method, url)) {
                    init = init || {};
                    var headers = new Headers(init.headers || (isRequestObj ? input.headers : undefined) || {});
                    if (!headers.has('X-CSRF-Token')) {
                        var token = getToken();
                        if (token) {
                            headers.set('X-CSRF-Token', token);
                        }
                    }
                    init.headers = headers;
                }
            } catch (e) {
                // 注入失败不应阻断原请求
                if (window.console && console.warn) {
                    console.warn('csrf: 注入请求头失败', e);
                }
            }
            return originalFetch.call(window, input, init);
        };
    }

    // ---- patch XMLHttpRequest ----
    if (typeof XMLHttpRequest !== 'undefined') {
        var originalOpen = XMLHttpRequest.prototype.open;
        var originalSend = XMLHttpRequest.prototype.send;

        XMLHttpRequest.prototype.open = function (method, url) {
            try {
                this.__csrfMethod = method;
                this.__csrfUrl = url;
            } catch (e) { /* 忽略 */ }
            return originalOpen.apply(this, arguments);
        };

        XMLHttpRequest.prototype.send = function () {
            try {
                if (needsToken(this.__csrfMethod, this.__csrfUrl)) {
                    var token = getToken();
                    if (token) {
                        this.setRequestHeader('X-CSRF-Token', token);
                    }
                }
            } catch (e) { /* 忽略 */ }
            return originalSend.apply(this, arguments);
        };
    }

    // 供业务代码在需要时直接取用
    window.getCSRFToken = getToken;
})();
