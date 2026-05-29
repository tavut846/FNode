<?php

namespace Tests\Feature;

use App\Models\User;
use App\Models\Plugin;
use Illuminate\Foundation\Testing\RefreshDatabase;
use Illuminate\Support\Facades\Schema;
use Tests\TestCase;

class SelfVerifyRouteTest extends TestCase
{
    use RefreshDatabase;

    protected function setUp(): void
    {
        parent::setUp();
        
        config()->set('app.key', 'base64:' . base64_encode(str_repeat('a', 32)));

        // Create the verify_status column if it doesn't exist (simulating plugin installation)
        if (!Schema::hasColumn('v2_user', 'verify_status')) {
            Schema::table('v2_user', function ($table) {
                $table->string('verify_status', 16)->nullable();
            });
        }

        // Enable the SelfVerify plugin in the test database
        Plugin::create([
            'code' => 'SelfVerify',
            'name' => 'SelfVerify Plugin',
            'version' => '1.0.0',
            'is_enabled' => true,
            'config' => json_encode([
                'verify_required' => true,
                'allow_invited' => false,
                'restore_trial_on_verify' => true,
            ]),
        ]);
    }

    public function test_self_verify_route_requires_authentication(): void
    {
        $response = $this->postJson('/api/v1/user/self-verify/send-code');
        
        // Unauthenticated request should be rejected by the user middleware
        $response->assertStatus(435); // 403 / 435 in Xboard depending on how auth/ApiException is configured
    }

    public function test_self_verify_route_registered_and_reachable(): void
    {
        $user = User::factory()->create([
            'verify_status' => 'pending',
        ]);

        // Mocking the Sanctum user authentication
        $response = $this->actingAs($user, 'sanctum')->postJson('/api/v1/user/self-verify/send-code');

        // It should hit the endpoint, not a 404, proving the route is registered and loaded.
        $this->assertNotEquals(404, $response->getStatusCode());
    }
}
