import { redirect } from '@sveltejs/kit';
import type { PageLoad } from './$types';

export const prerender = false;

export const load: PageLoad = () => {
    const organizations: { role: string }[] = JSON.parse(
        localStorage.getItem('USER_ORGANIZATIONS') || '[]'
    );
    const canManage = organizations.some((o) => o.role === 'owner' || o.role === 'admin');
    if (!canManage) {
        throw redirect(302, '/');
    }
    return {};
};
